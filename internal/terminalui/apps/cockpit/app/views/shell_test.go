package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func TestHeaderBarNode_IsValidCoreNode(t *testing.T) {
	t.Parallel()

	node := HeaderBarNode("ready", "127.0.0.1")
	if node.Kind() != core.KindText {
		t.Fatalf("Kind = %v, want KindText", node.Kind())
	}
	if !strings.Contains(node.ContentValue().Text, "ready") {
		t.Fatalf("text missing left side: %q", node.ContentValue().Text)
	}
	if !strings.Contains(node.ContentValue().Text, "127.0.0.1") {
		t.Fatalf("text missing right side: %q", node.ContentValue().Text)
	}
}

func TestHeaderBar_MeasureTracksIntrinsicWidth(t *testing.T) {
	t.Parallel()

	w := HeaderBar("ready", "127.0.0.1")
	layoutNode := retained.Materialize(&retained.Context[state.Model]{Model: func() state.Model { return state.Model{} }}, w)
	size := layoutNode.Measure(geom.Unbounded(), &layout.LayoutContext{})
	want := headerIntrinsicWidth("ready", "127.0.0.1")

	if size.W != want {
		t.Fatalf("measure width = %d, want %d", size.W, want)
	}
	if size.H != 1 {
		t.Fatalf("measure height = %d, want %d", size.H, 1)
	}
}
