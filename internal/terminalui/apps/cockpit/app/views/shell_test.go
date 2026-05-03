package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
)

func TestHeaderBar_MeasureTracksSharedPresentation(t *testing.T) {
	t.Parallel()

	w := HeaderBar("ready", "127.0.0.1")
	layoutNode := view.Materialize(&view.Context[state.Model]{Model: func() state.Model { return state.Model{} }}, w)
	size := layoutNode.Measure(geom.Unbounded(), &layout.LayoutContext{})
	want := headerIntrinsicWidth("ready", "127.0.0.1")

	if size.W != want {
		t.Fatalf("measure width = %d, want %d", size.W, want)
	}
	if size.H != 1 {
		t.Fatalf("measure height = %d, want 1", size.H)
	}
}

func TestHeaderBar_PaintUsesSharedPresentation(t *testing.T) {
	t.Parallel()

	w := HeaderBar("ready", "127.0.0.1")
	layoutNode := view.Materialize(&view.Context[state.Model]{Model: func() state.Model { return state.Model{} }}, w)
	node := &layout.LayoutNode{
		ID:         1,
		BorderRect: geom.Rect{W: 40, H: 1},
	}
	buf := paint.NewBuffer(geom.Rect{W: 40, H: 1})

	layoutNode.Paint(buf, node, &layout.PaintContext{})

	if got, want := buf.String(), renderHeaderLine(40, "ready", "127.0.0.1"); got != want {
		t.Fatalf("paint = %q, want %q", got, want)
	}
}
