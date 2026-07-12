package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

func paintLayoutTree(node *layout.LayoutNode, painter paint.Painter, ctx *layout.PaintContext, parentOrigin geom.Point) {
	if node == nil || node.ClipRect.Empty() || node.BorderRect.Empty() {
		return
	}
	scoped := painter.WithClip(node.ClipRect).WithOrigin(geom.Point{
		X: node.BorderRect.X - parentOrigin.X,
		Y: node.BorderRect.Y - parentOrigin.Y,
	})
	node.RenderNode.Paint(scoped, node, ctx)
	for _, child := range node.Kids {
		paintLayoutTree(child, scoped, ctx, geom.Point{X: node.BorderRect.X, Y: node.BorderRect.Y})
	}
}
