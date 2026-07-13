package loop

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func (loop *AppLoop[M]) SetFocus(next *layout.LayoutNode) {
	if next != nil && !canFocus(next) {
		next = nil
	}
	prev := loop.Focused
	if prev == next {
		return
	}
	var actions []update.Action
	if prev != nil {
		if hooks, ok := prev.RenderNode.(interaction.FocusEvents); ok {
			actions = append(actions, hooks.OnBlur(prev)...)
		}
	}
	loop.Focused = next
	if next != nil {
		loop.focusedID = next.FocusID
	} else {
		loop.focusedID = ""
	}
	loop.Invalidate()
	if next != nil {
		if hooks, ok := next.RenderNode.(interaction.FocusEvents); ok {
			actions = append(actions, hooks.OnFocus(next)...)
		}
	}
	if len(actions) > 0 {
		loop.Dispatch(actions)
	}
}

func (loop *AppLoop[M]) FocusNext() {
	focusables := loop.focusOrder()
	if len(focusables) == 0 {
		loop.SetFocus(nil)
		return
	}
	if loop.Focused == nil {
		loop.SetFocus(focusables[0])
		return
	}
	for i, node := range focusables {
		if node.ID != loop.Focused.ID {
			continue
		}
		loop.SetFocus(focusables[(i+1)%len(focusables)])
		return
	}
	loop.SetFocus(focusables[0])
}

func (loop *AppLoop[M]) FocusPrev() {
	focusables := loop.focusOrder()
	if len(focusables) == 0 {
		loop.SetFocus(nil)
		return
	}
	if loop.Focused == nil {
		loop.SetFocus(focusables[len(focusables)-1])
		return
	}
	for i, node := range focusables {
		if node.ID != loop.Focused.ID {
			continue
		}
		loop.SetFocus(focusables[(i-1+len(focusables))%len(focusables)])
		return
	}
	loop.SetFocus(focusables[len(focusables)-1])
}

// focusOrder returns focusable nodes in order.
//
// The semantic FocusGraph is the sole source of focus identity and
// traversal order. When no graph is compiled (legacy retained.ViewSpec
// path without core.Node migration), we derive order from the render tree
// as a compatibility fallback.
//
// TODO(v2-migration): delete renderTreeFocusOrder once all views use
// core.Node, which guarantees FocusGraph compilation. Tracked in
// `migrationTracker` (52 files remaining as of 2026-06-15).
func (loop *AppLoop[M]) focusOrder() []*layout.LayoutNode {
	if loop.focusGraph.Empty() {
		return renderTreeFocusOrder(loop.Tree)
	}
	return loop.semanticFocusOrder()
}

func (loop *AppLoop[M]) semanticFocusOrder() []*layout.LayoutNode {
	var nodes []*layout.LayoutNode
	for _, fid := range loop.focusGraph.Order {
		if node := loop.findByFocusID(string(fid)); node != nil && canFocus(node) {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func (loop *AppLoop[M]) findByFocusID(fid string) *layout.LayoutNode {
	var result *layout.LayoutNode
	var walk func(*layout.LayoutNode)
	walk = func(node *layout.LayoutNode) {
		if node == nil {
			return
		}
		if node.FocusID == fid {
			result = node
			return
		}
		for _, child := range stableKids(node.Kids) {
			walk(child)
			if result != nil {
				return
			}
		}
	}
	walk(loop.Tree)
	return result
}

func renderTreeFocusOrder(root *layout.LayoutNode) []*layout.LayoutNode {
	var nodes []*layout.LayoutNode
	var walk func(*layout.LayoutNode)
	walk = func(node *layout.LayoutNode) {
		if node == nil {
			return
		}
		if canFocus(node) {
			nodes = append(nodes, node)
		}
		for _, child := range stableKids(node.Kids) {
			walk(child)
		}
	}
	walk(root)
	return nodes
}

func survivingFocusableFromLineage(previous *layout.LayoutNode, nodes map[layout.NodeID]*layout.LayoutNode) *layout.LayoutNode {
	for cursor := previous; cursor != nil; cursor = cursor.Parent {
		current := nodes[cursor.ID]
		if canFocus(current) {
			return current
		}
		if descendant := firstFocusableDescendant(current); descendant != nil {
			return descendant
		}
	}
	return nil
}

func firstFocusableDescendant(root *layout.LayoutNode) *layout.LayoutNode {
	if root == nil {
		return nil
	}
	for _, child := range stableKids(root.Kids) {
		if canFocus(child) {
			return child
		}
		if descendant := firstFocusableDescendant(child); descendant != nil {
			return descendant
		}
	}
	return nil
}

func nearestFocusable(node *layout.LayoutNode) *layout.LayoutNode {
	for current := node; current != nil; current = current.Parent {
		if canFocus(current) {
			return current
		}
	}
	return nil
}

func canFocus(node *layout.LayoutNode) bool {
	if node == nil {
		return false
	}
	focusable, ok := node.RenderNode.(interaction.Focusable)
	return ok && focusable.CanFocus(node)
}

func (loop *AppLoop[M]) repairFocus(nodes map[layout.NodeID]*layout.LayoutNode, previousFocusedIndex int) {
	if loop.focusedID != "" {
		for _, node := range nodes {
			if node.FocusID == loop.focusedID && canFocus(node) {
				loop.Focused = node
				return
			}
		}
	}
	if loop.Focused == nil {
		focusables := loop.focusOrder()
		if len(focusables) > 0 {
			loop.Focused = focusables[0]
			loop.focusedID = focusables[0].FocusID
			if hooks, ok := loop.Focused.RenderNode.(interaction.FocusEvents); ok {
				if actions := hooks.OnFocus(loop.Focused); len(actions) > 0 {
					loop.Dispatch(actions)
				}
			}
		}
		return
	}
	next := nodes[loop.Focused.ID]
	if next == nil || !canFocus(next) {
		if lineage := survivingFocusableFromLineage(loop.Focused, nodes); lineage != nil {
			loop.Focused = lineage
			loop.focusedID = lineage.FocusID
			return
		}
		focusables := loop.focusOrder()
		if len(focusables) == 0 {
			loop.Focused = nil
			loop.focusedID = ""
			return
		}
		if previousFocusedIndex < 0 {
			loop.Focused = focusables[0]
			loop.focusedID = focusables[0].FocusID
			return
		}
		if previousFocusedIndex >= len(focusables) {
			previousFocusedIndex = len(focusables) - 1
		}
		loop.Focused = focusables[previousFocusedIndex]
		loop.focusedID = focusables[previousFocusedIndex].FocusID
		return
	}
	loop.Focused = next
	loop.focusedID = next.FocusID
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
