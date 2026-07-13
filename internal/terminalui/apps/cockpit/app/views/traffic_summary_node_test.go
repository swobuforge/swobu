package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestBuildTrafficSummaryNode_ReturnsCoreNode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model state.Model
		want  string
	}{
		{
			name: "current summary",
			model: state.Model{
				CurrentEndpoint: "acme",
				TrafficRows: []state.TrafficRow{
					{
						Result:          "success",
						StatusCode:      200,
						DurMillis:       intPtr(10),
						InputTokens:     intPtr(100),
						OutputTokens:    intPtr(20),
						CacheReadTokens: intPtr(40),
					},
				},
			},
			want: "1 req · ok 100% · p95 10 ms · cache 40% (coverage 100%) · in 100 / out 20",
		},
		{
			name: "no traffic yet",
			model: state.Model{
				CurrentEndpoint: "acme",
			},
			want: "no traffic yet",
		},
		{
			name: "stale snapshot",
			model: state.Model{
				CurrentEndpoint: "acme",
				TrafficError:    "stale snapshot",
			},
			want: "stale snapshot",
		},
		{
			name: "empty create lane",
			model: state.Model{
				CurrentEndpoint: "",
			},
			want: "empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			node := BuildTrafficSummaryNode(tt.model)
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

			tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 96, H: 4})
			buf := paint.NewBuffer(geom.Rect{W: 96, H: 4})
			paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
			out := strings.TrimSpace(buf.String())

			if out != tt.want {
				t.Fatalf("render = %q, want %q", out, tt.want)
			}
		})
	}
}
