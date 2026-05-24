package testharness

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
	"github.com/swobuforge/swobu/testscreen/buf"
)

// RenderSpec materializes and paints one retained ViewSpec into a viewport.
// It is intentionally root-agnostic: callers provide any model and any spec.
func RenderSpec[M any](model M, spec retained.ViewSpec[M], viewport geom.Rect) buf.View {
	ctx := &retained.Context[M]{Model: func() M { return model }}
	node := retained.Materialize(ctx, spec)
	layoutNode := &layout.LayoutNode{ID: 1, BorderRect: viewport}
	backbuffer := paint.NewBuffer(viewport)
	node.Paint(backbuffer, layoutNode, &layout.PaintContext{})
	return buf.FromStringWithViewport(backbuffer.String(), viewport.W, viewport.H)
}
