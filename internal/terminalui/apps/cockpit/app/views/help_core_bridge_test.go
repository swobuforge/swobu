package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/reconcile"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func TestBuildHelpSection_RendersCoreBridgeRows(t *testing.T) {
	t.Parallel()

	model := state.Model{}
	ctx := &retained.Context[state.Model]{
		Local: reconcile.NewLocalStore().Scope(1),
		Model: func() state.Model { return model },
	}
	spec := BuildHelpSection(ctx)
	node := retained.Materialize(ctx, spec)
	tree := (&layout.TreeBuilder{}).Build(node, geom.Rect{W: 80, H: 8})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 8})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String()) // swobu:io-string source=domain
	if !strings.Contains(out, "ask question") {
		t.Fatalf("render = %q, want ask question row", out)
	}
	if !strings.Contains(out, "open ↵") {
		t.Fatalf("render = %q, want open action label", out)
	}
}
