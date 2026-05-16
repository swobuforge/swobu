package loop

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func (loop *AppLoop[M]) Invalidate() {
	loop.invalidated = true
}

func (loop *AppLoop[M]) NeedsRebuild() bool {
	return loop.invalidated || loop.Tree == nil
}

// Rebuild reconciles the typed root view into a retained structural tree
// and then resolves layout from the given terminal bounds.
func (loop *AppLoop[M]) Rebuild(root retained.ViewSpec[M], bounds geom.Rect) {
	effects := loop.RebuildPending(root, bounds)
	for _, eff := range effects {
		loop.executeEffect(eff)
	}
}

// RebuildPending reconciles and lays out the retained tree, and returns
// lifecycle effects from mount/unmount hooks.
func (loop *AppLoop[M]) RebuildPending(root retained.ViewSpec[M], bounds geom.Rect) []update.Effect {
	if !loop.invalidated && loop.Tree != nil {
		return nil
	}
	previous := loop.Tree
	previousFocusOrder := focusOrder(previous)
	previousFocusedIndex := focusIndex(previousFocusOrder, loop.Focused)
	previousByID := make(map[layout.NodeID]*layout.LayoutNode)
	collectByID(previous, previousByID)
	rootNode, _, _, lifecycle := loop.reconciler.Reconcile(
		root,
		&loop.Model,
		func(action update.Action) {
			loop.Dispatch([]update.Action{action})
		},
		func(action update.Action) {
			loop.Dispatch([]update.Action{action})
		},
	)
	loop.Tree = (&layout.TreeBuilder{}).Build(rootNode, bounds)
	currentByID := make(map[layout.NodeID]*layout.LayoutNode)
	collectByID(loop.Tree, currentByID)
	loop.repairFocus(currentByID, previousFocusedIndex)
	loop.applyPendingFocusKey()
	loop.ensureFocusVisible(rootNode, bounds)

	loop.invalidated = false
	return lifecycle
}

func focusIndex(nodes []*layout.LayoutNode, target *layout.LayoutNode) int {
	if target == nil {
		return -1
	}
	for i, node := range nodes {
		if node != nil && node.ID == target.ID {
			return i
		}
	}
	return -1
}

func collectByID(node *layout.LayoutNode, out map[layout.NodeID]*layout.LayoutNode) {
	if node == nil {
		return
	}
	out[node.ID] = node
	for _, child := range node.Kids {
		collectByID(child, out)
	}
}

func (loop *AppLoop[M]) applyPendingFocusKey() {
	key := strings.TrimSpace(loop.pendingFocusKey) // trimlowerlint:allow boundary canonicalization
	if key == "" || loop.Tree == nil {
		return
	}
	node := firstFocusableByKey(loop.Tree, key)
	if node == nil {
		return
	}
	loop.pendingFocusKey = ""
	loop.SetFocus(node)
}
