package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/reconcile"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func TestBuildClientsStaticSummaryNode_ReturnsCoreNode(t *testing.T) {
	t.Parallel()

	node := BuildClientsStaticSummaryNode()
	if diags := core.Validate(node); len(diags) > 0 {
		t.Fatalf("validation failed: %v", diags)
	}

	renderNode, err := corelower.Lower(node, corelower.EnvConfig{}, func(a state.Action) update.Action {
		return a
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if renderNode == nil {
		t.Fatal("expected render node")
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 80, H: 4})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 4})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String())

	if out != "not set" {
		t.Fatalf("render = %q, want not set", out)
	}
}

func TestBuildClientsSection_UsesCoreStaticSummaryForFallbackStates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		model state.Model
	}{
		{
			name:  "no endpoint",
			model: state.Model{},
		},
		{
			name:  "busy save",
			model: state.Model{InteractionMode: state.InteractionModeBusySave},
		},
		{
			name: "saved shell",
			model: state.Model{
				HeaderStatus:    "saved",
				Endpoints:       []string{"acme"},
				CurrentEndpoint: "acme",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := &retained.Context[state.Model]{
				Local: reconcile.NewLocalStore().Scope(1),
				Model: func() state.Model { return tc.model },
			}
			out := renderCockpitView(t, ctx, retained.Build[state.Model](BuildClientsSection))
			if !strings.Contains(out, "clients") {
				t.Fatalf("render = %q, want clients section", out)
			}
			if !strings.Contains(out, "not set") {
				t.Fatalf("render = %q, want static not set summary", out)
			}
		})
	}
}
