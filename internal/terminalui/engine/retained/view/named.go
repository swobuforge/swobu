package view

import (
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

type namedRenderNodeMeta struct {
	RenderNode
	key       string
	lifecycle LifecycleEffects
}

// Forward interaction interface methods so the reconciler/runtime can find
// handlers through the Named wrapper.
func (k namedRenderNodeMeta) HitTest(local geom.Point, node *layout.LayoutNode) bool {
	if h, ok := k.RenderNode.(interaction.Hittable); ok {
		return h.HitTest(local, node)
	}
	return false
}
func (k namedRenderNodeMeta) HandleEvent(ev interaction.Event, node *layout.LayoutNode) []update.Action {
	if h, ok := k.RenderNode.(interaction.EventHandler); ok {
		return h.HandleEvent(ev, node)
	}
	return nil
}
func (k namedRenderNodeMeta) HandleScopedEvent(ev interaction.Event, node *layout.LayoutNode) (bool, []update.Action) {
	if h, ok := k.RenderNode.(interaction.ScopedEventHandler); ok {
		return h.HandleScopedEvent(ev, node)
	}
	if h, ok := k.RenderNode.(interaction.EventHandler); ok {
		actions := h.HandleEvent(ev, node)
		return len(actions) > 0, actions
	}
	return false, nil
}
func (k namedRenderNodeMeta) CanFocus(node *layout.LayoutNode) bool {
	if f, ok := k.RenderNode.(interaction.Focusable); ok {
		return f.CanFocus(node)
	}
	return false
}
func (k namedRenderNodeMeta) OnFocus(node *layout.LayoutNode) []update.Action {
	if f, ok := k.RenderNode.(interaction.FocusEvents); ok {
		return f.OnFocus(node)
	}
	return nil
}
func (k namedRenderNodeMeta) OnBlur(node *layout.LayoutNode) []update.Action {
	if f, ok := k.RenderNode.(interaction.FocusEvents); ok {
		return f.OnBlur(node)
	}
	return nil
}
func (k namedRenderNodeMeta) OnMount(node *layout.LayoutNode) []update.Action {
	if l, ok := k.RenderNode.(interaction.Lifecycle); ok {
		return l.OnMount(node)
	}
	return nil
}
func (k namedRenderNodeMeta) OnUnmount(node *layout.LayoutNode) []update.Action {
	if l, ok := k.RenderNode.(interaction.Lifecycle); ok {
		return l.OnUnmount(node)
	}
	return nil
}

// Forward composite interface so named wrappers preserve structural traversal.
func (k namedRenderNodeMeta) VisitChildren(visit func(hint string, child layout.RenderNode)) {
	if c, ok := k.RenderNode.(layout.Composite); ok {
		c.VisitChildren(visit)
	}
}

func (k namedRenderNodeMeta) MapChildren(rewrite func(hint string, child layout.RenderNode) layout.RenderNode) layout.RenderNode {
	c, ok := k.RenderNode.(layout.Composite)
	if !ok {
		return k
	}
	clone := k
	clone.RenderNode = c.MapChildren(rewrite)
	return clone
}

// renderNamed composes a child view under explicit sibling identity continuity.
// It also scopes child-local hook state under the name.
func renderNamed[M any](ctx *Context[M], name string, child ViewSpec[M]) RenderNode {
	scope := ctx.Local
	if name != "" {
		scope = ctx.Local.WithPrefix(name)
	}
	node := buildViewNode(child, scope, ctx.dispatch, ctx.emit, ctx.Model)
	if node == nil {
		return nil
	}
	return namedRenderNodeMeta{
		RenderNode: node,
		key:        name,
		lifecycle:  captureLifecycle(child),
	}
}

// Named wraps one child under parent-scoped identity continuity.
func Named[M any](name string, child ViewSpec[M]) ViewSpec[M] {
	return View[M](func(ctx *Context[M]) RenderNode {
		return renderNamed(ctx, name, child)
	})
}

// BuildViewRootNode is the private reconciler seam for building the typed root view.
func BuildViewRootNode[M any](root ViewSpec[M], local LocalScope, dispatch func(update.Action), emit func(update.Action), model func() M) RenderNode {
	return buildViewNode(root, local, dispatch, emit, model)
}

func NamedNodeMetadata(node RenderNode) (RenderNode, string, LifecycleEffects) {
	meta, ok := node.(namedRenderNodeMeta)
	if !ok {
		return node, "", LifecycleEffects{}
	}
	return meta.RenderNode, meta.key, meta.lifecycle
}
