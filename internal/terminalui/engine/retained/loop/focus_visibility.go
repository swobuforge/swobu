package loop

import (
	"reflect"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

// ensureFocusVisible enforces one runtime-level invariant:
// focused nodes must remain inside every scroll-ancestor viewport.
//
// The routine mutates structural scroll offsets, then re-lays out the tree.
func (loop *AppLoop[M]) ensureFocusVisible(root layout.RenderNode, bounds geom.Rect) {
	if loop.Tree == nil || loop.Focused == nil || root == nil {
		return
	}
	// Keep bounded: one focus move should need only a handful of ancestor
	// adjustments even with nested scroll scopes.
	for i := 0; i < 4; i++ {
		if !scrollFocusIntoAncestors(loop.Focused) {
			return
		}
		loop.Tree = (&layout.TreeBuilder{}).Build(root, bounds)
		nodes := make(map[layout.NodeID]*layout.LayoutNode)
		collectByID(loop.Tree, nodes)
		if loop.Focused != nil {
			loop.Focused = nodes[loop.Focused.ID]
		}
		if loop.Focused == nil {
			return
		}
	}
}

func scrollFocusIntoAncestors(focused *layout.LayoutNode) bool {
	if focused == nil {
		return false
	}
	changed := false
	for ancestor := focused.Parent; ancestor != nil; ancestor = ancestor.Parent {
		scrollNode := extractScrollYRenderNode(ancestor.RenderNode)
		if scrollNode == nil {
			continue
		}
		viewport := ancestor.ViewportRect
		if viewport.Empty() {
			continue
		}
		want := ancestor.ScrollOffset.Y
		if focused.BorderRect.Y < viewport.Y {
			want -= viewport.Y - focused.BorderRect.Y
		}
		if focused.BorderRect.Bottom() > viewport.Bottom() {
			want += focused.BorderRect.Bottom() - viewport.Bottom()
		}
		maxOffset := ancestor.ContentSize.H - viewport.H
		if maxOffset < 0 {
			maxOffset = 0
		}
		if want < 0 {
			want = 0
		}
		if want > maxOffset {
			want = maxOffset
		}
		if want == ancestor.ScrollOffset.Y {
			continue
		}
		scrollNode.Offset = want
		changed = true
	}
	return changed
}

func extractScrollYRenderNode(node layout.RenderNode) *layout.ScrollYRenderNode {
	if node == nil {
		return nil
	}
	if scroll, ok := node.(*layout.ScrollYRenderNode); ok {
		return scroll
	}
	v := reflect.ValueOf(node)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.IsValid() || !field.CanInterface() {
			continue
		}
		child, ok := field.Interface().(layout.RenderNode)
		if !ok {
			continue
		}
		if scroll := extractScrollYRenderNode(child); scroll != nil {
			return scroll
		}
	}
	return nil
}
