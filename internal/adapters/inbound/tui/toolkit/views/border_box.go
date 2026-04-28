package views

import (
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/geom"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/layout"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
)

// NewBorderBox wraps a child in a single-line ASCII border box.
func NewBorderBox[M any](child view.ViewSpec[M]) view.ViewSpec[M] {
	return view.View[M](func(ctx *view.Context[M]) view.RenderNode {
		childNode := view.Materialize(ctx, child)
		box := layout.NewBox(childNode)
		box.Border = &layout.SingleBorder
		box.Padding = geom.Insets{}
		return box
	})
}
