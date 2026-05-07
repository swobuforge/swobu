package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// NewBorderBox wraps a child in a single-line ASCII border box.
func NewBorderBox[M any](child retained.ViewSpec[M]) retained.ViewSpec[M] {
	return retained.View[M](func(ctx *retained.Context[M]) retained.RenderNode {
		childNode := retained.Materialize(ctx, child)
		box := layout.NewBox(childNode)
		box.Border = &layout.SingleBorder
		box.Padding = geom.Insets{}
		return box
	})
}
