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
	focusables := focusOrder(loop.Tree)
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
	focusables := focusOrder(loop.Tree)
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

func (loop *AppLoop[M]) repairFocus(nodes map[layout.NodeID]*layout.LayoutNode, previousFocusedIndex int) {
	if loop.Focused == nil {
		focusables := focusOrder(loop.Tree)
		if len(focusables) > 0 {
			loop.Focused = focusables[0]
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
			return
		}
		focusables := focusOrder(loop.Tree)
		if len(focusables) == 0 {
			loop.Focused = nil
			return
		}
		if previousFocusedIndex < 0 {
			loop.Focused = focusables[0]
			return
		}
		if previousFocusedIndex >= len(focusables) {
			previousFocusedIndex = len(focusables) - 1
		}
		loop.Focused = focusables[previousFocusedIndex]
		return
	}
	loop.Focused = next
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

func focusOrder(root *layout.LayoutNode) []*layout.LayoutNode {
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
