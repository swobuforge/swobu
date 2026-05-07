// local state retention, view lifecycle hooks, and mount bookkeeping in one
// engine seam.
package reconcile

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type StateKey struct {
	NodeID  layout.NodeID
	SlotKey string
}

type LocalStore struct {
	values map[StateKey]any
}

func NewLocalStore() *LocalStore {
	return &LocalStore{values: make(map[StateKey]any)}
}

func (s *LocalStore) Scope(nodeID layout.NodeID) retained.LocalScope {
	return localScope{store: s, nodeID: nodeID, prefix: ""}
}

func (s *LocalStore) DeleteNode(nodeID layout.NodeID) {
	for key := range s.values {
		if key.NodeID == nodeID {
			delete(s.values, key)
		}
	}
}

func (s *LocalStore) DeletePrefix(nodeID layout.NodeID, prefix string) {
	if prefix == "" {
		return
	}
	for key := range s.values {
		if key.NodeID == nodeID && strings.HasPrefix(key.SlotKey, prefix) {
			delete(s.values, key)
		}
	}
}

type localScope struct {
	store  *LocalStore
	nodeID layout.NodeID
	prefix string
}

func (s localScope) Get(slot int) (any, bool) {
	key := s.prefix + strconv.Itoa(slot)
	value, ok := s.store.values[StateKey{NodeID: s.nodeID, SlotKey: key}]
	return value, ok
}

func (s localScope) Set(slot int, value any) {
	key := s.prefix + strconv.Itoa(slot)
	s.store.values[StateKey{NodeID: s.nodeID, SlotKey: key}] = value
}

func (s localScope) WithPrefix(prefix string) retained.LocalScope {
	return localScope{store: s.store, nodeID: s.nodeID, prefix: s.prefix + prefix + "/"}
}

type Reconciler[M any] struct {
	locals *LocalStore
	root   *RetainedRenderNode
	nextID layout.NodeID
}

// ViewRenderNode is the ephemeral declarative tree produced during one rebuild.
// Retained identity is carried in the parallel RetainedRenderNode graph.
type ViewRenderNode struct {
	Hint       string
	Key        string
	RenderNode layout.RenderNode
	Retained   *RetainedRenderNode
	Children   []ViewNodeChild
}

type ViewNodeChild struct {
	Hint string
	Node *ViewRenderNode
}

// RetainedRenderNode is the durable identity tree for one reconciled rebuild.
// It owns the stable NodeID and child continuity graph; ViewRenderNodes are disposable.
type RetainedRenderNode struct {
	ID        layout.NodeID
	Hint      string
	Key       string
	Lifecycle retained.LifecycleEffects
	Children  []*RetainedRenderNode
}

func New[M any](locals *LocalStore) *Reconciler[M] {
	return &Reconciler[M]{
		locals: locals,
		nextID: 1,
	}
}

// component builds, and local-state carryover in one ordered pass.
func (r *Reconciler[M]) Reconcile(
	root retained.ViewSpec[M],
	model *M,
	dispatch func(update.Action),
	emit func(update.Action),
) (layout.RenderNode, []layout.NodeID, []layout.NodeID, []update.Effect) {
	reused := make(map[layout.NodeID]struct{})
	var mounts []layout.NodeID

	claim := func(previous *RetainedRenderNode, hint, key string) *RetainedRenderNode {
		if previous != nil {
			reused[previous.ID] = struct{}{}
			return &RetainedRenderNode{ID: previous.ID, Hint: hint, Key: key}
		}
		retainedNode := &RetainedRenderNode{ID: r.nextID, Hint: hint, Key: key}
		r.nextID++
		mounts = append(mounts, retainedNode.ID)
		return retainedNode
	}

	tag := func(retained *RetainedRenderNode, renderNode layout.RenderNode) layout.RenderNode {
		if _, ok := layout.IdentityOf(renderNode); ok {
			return renderNode
		}
		return layout.WithIdentity(retained.ID, renderNode)
	}

	var buildComponent func(previous *RetainedRenderNode, hint string, root retained.ViewSpec[M]) *ViewRenderNode
	var buildNode func(previous *RetainedRenderNode, hint string, renderNode layout.RenderNode) *ViewRenderNode
	var buildResolved func(previous *RetainedRenderNode, retained *RetainedRenderNode, hint string, renderNode layout.RenderNode) *ViewRenderNode
	var materialize func(node *ViewRenderNode) layout.RenderNode

	buildComponent = func(previous *RetainedRenderNode, hint string, root retained.ViewSpec[M]) *ViewRenderNode {
		retainedNode := claim(previous, hint, "")
		renderNode := retained.BuildViewRootNode(root, r.locals.Scope(retainedNode.ID), dispatch, emit, func() M { return *model })
		_, key, lifecycle := retained.NamedNodeMetadata(renderNode)
		retainedNode.Key = key
		retainedNode.Lifecycle = lifecycle
		rootLifecycle := retained.CaptureLifecycle(root)
		retainedNode.Lifecycle.OnMount = append(rootLifecycle.OnMount, retainedNode.Lifecycle.OnMount...)
		retainedNode.Lifecycle.OnUnmount = append(rootLifecycle.OnUnmount, retainedNode.Lifecycle.OnUnmount...)
		return buildResolved(previous, retainedNode, hint, renderNode)
	}

	buildNode = func(previous *RetainedRenderNode, hint string, renderNode layout.RenderNode) *ViewRenderNode {
		_, key, lifecycle := retained.NamedNodeMetadata(renderNode)
		retainedNode := claim(previous, hint, key)
		retainedNode.Lifecycle = lifecycle
		return buildResolved(previous, retainedNode, hint, renderNode)
	}

	buildResolved = func(previous *RetainedRenderNode, retainedNode *RetainedRenderNode, hint string, renderNode layout.RenderNode) *ViewRenderNode {
		tagged := tag(retainedNode, renderNode)
		viewNode := &ViewRenderNode{
			Hint:       hint,
			RenderNode: tagged,
			Retained:   retainedNode,
		}

		composite, ok := renderNode.(layout.Composite)
		if !ok {
			return viewNode
		}

		matcher := newSiblingMatcher(previous)
		seenKeys := make(map[string]struct{})
		composite.VisitChildren(func(childHint string, child layout.RenderNode) {
			_, key, _ := retained.NamedNodeMetadata(child)
			if key != "" {
				if _, exists := seenKeys[key]; exists {
					panic(fmt.Sprintf("duplicate sibling key %q under %q", key, hint))
				}
				seenKeys[key] = struct{}{}
			}
			childNode := buildNode(matcher.match(childHint, key), childHint, child)
			viewNode.Children = append(viewNode.Children, ViewNodeChild{
				Hint: childHint,
				Node: childNode,
			})
			retainedNode.Children = append(retainedNode.Children, childNode.Retained)
		})
		return viewNode
	}

	materialize = func(node *ViewRenderNode) layout.RenderNode {
		inner := layout.UnwrapIdentity(node.RenderNode)
		identityID, hasIdentity := layout.IdentityOf(node.RenderNode)

		composite, ok := inner.(layout.Composite)
		if !ok {
			return node.RenderNode
		}
		index := 0
		rewritten := composite.MapChildren(func(_ string, _ layout.RenderNode) layout.RenderNode {
			child := node.Children[index]
			index++
			return materialize(child.Node)
		})
		if hasIdentity {
			return layout.WithIdentity(identityID, rewritten)
		}
		return rewritten
	}

	rootView := buildComponent(r.root, "root", root)
	rootNode := materialize(rootView)

	var unmounts []layout.NodeID
	var lifecycle []update.Effect
	collectUnmounts(r.root, nil, reused, &unmounts, r.locals)
	collectLifecycleFromRetained(rootView.Retained, r.root, reused, &lifecycle)
	r.root = rootView.Retained

	return rootNode, mounts, unmounts, lifecycle
}

type siblingMatcher struct {
	byKey  map[string][]*RetainedRenderNode
	byHint map[string][]*RetainedRenderNode
}

func newSiblingMatcher(parent *RetainedRenderNode) siblingMatcher {
	matcher := siblingMatcher{
		byKey:  make(map[string][]*RetainedRenderNode),
		byHint: make(map[string][]*RetainedRenderNode),
	}
	if parent == nil {
		return matcher
	}
	for _, child := range parent.Children {
		if child == nil {
			continue
		}
		if child.Key != "" {
			if _, exists := matcher.byKey[child.Key]; exists {
				panic(fmt.Sprintf("duplicate retained sibling key %q under %q", child.Key, parent.Hint))
			}
			matcher.byKey[child.Key] = append(matcher.byKey[child.Key], child)
			continue
		}
		matcher.byHint[child.Hint] = append(matcher.byHint[child.Hint], child)
	}
	return matcher
}

func (m *siblingMatcher) match(hint, key string) *RetainedRenderNode {
	if key != "" {
		return takeFirst(m.byKey, key)
	}
	return takeFirst(m.byHint, hint)
}

func takeFirst(groups map[string][]*RetainedRenderNode, name string) *RetainedRenderNode {
	group := groups[name]
	if len(group) == 0 {
		return nil
	}
	match := group[0]
	groups[name] = group[1:]
	return match
}

func collectLifecycleFromRetained(next *RetainedRenderNode, previous *RetainedRenderNode, reused map[layout.NodeID]struct{}, out *[]update.Effect) {
	collectMountLifecycle(next, reused, out)
	collectUnmountLifecycle(previous, reused, out)
}

func collectMountLifecycle(root *RetainedRenderNode, reused map[layout.NodeID]struct{}, out *[]update.Effect) {
	if root == nil {
		return
	}
	if _, ok := reused[root.ID]; !ok && len(root.Lifecycle.OnMount) > 0 {
		*out = append(*out, root.Lifecycle.OnMount...)
	}
	for _, child := range root.Children {
		collectMountLifecycle(child, reused, out)
	}
}

func collectUnmountLifecycle(root *RetainedRenderNode, reused map[layout.NodeID]struct{}, out *[]update.Effect) {
	if root == nil {
		return
	}
	if _, ok := reused[root.ID]; !ok && len(root.Lifecycle.OnUnmount) > 0 {
		*out = append(*out, root.Lifecycle.OnUnmount...)
	}
	for _, child := range root.Children {
		collectUnmountLifecycle(child, reused, out)
	}
}

func collectUnmounts(root *RetainedRenderNode, parent *RetainedRenderNode, reused map[layout.NodeID]struct{}, unmounts *[]layout.NodeID, locals *LocalStore) {
	if root == nil {
		return
	}
	if _, ok := reused[root.ID]; !ok {
		*unmounts = append(*unmounts, root.ID)
		locals.DeleteNode(root.ID)
		if parent != nil && root.Key != "" {
			locals.DeletePrefix(parent.ID, root.Key+"/")
		}
	}
	for _, child := range root.Children {
		collectUnmounts(child, root, reused, unmounts, locals)
	}
}
