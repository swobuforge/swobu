package loop

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

// Render paints the current retained tree into a fresh backbuffer using the
// current app-loop focus state.
func (loop *AppLoop[M]) Render(bounds geom.Rect) *paint.BufferPainter {
	buffer := paint.NewBuffer(bounds)
	if loop.Tree == nil {
		return buffer
	}
	paintTree(loop.Tree, buffer, &layout.PaintContext{FocusedID: focusedID(loop.Focused)}, geom.Point{})
	return buffer
}

func paintTree(node *layout.LayoutNode, painter paint.Painter, ctx *layout.PaintContext, parentOrigin geom.Point) {
	if node == nil || node.ClipRect.Empty() || node.BorderRect.Empty() {
		return
	}
	scoped := painter.WithClip(node.ClipRect).WithOrigin(geom.Point{
		X: node.BorderRect.X - parentOrigin.X,
		Y: node.BorderRect.Y - parentOrigin.Y,
	})
	node.RenderNode.Paint(scoped, node, ctx)
	for _, child := range stableKids(node.Kids) {
		paintTree(child, scoped, ctx, geom.Point{X: node.BorderRect.X, Y: node.BorderRect.Y})
	}
}

func focusedID(node *layout.LayoutNode) layout.NodeID {
	if node == nil {
		return 0
	}
	return node.ID
}
