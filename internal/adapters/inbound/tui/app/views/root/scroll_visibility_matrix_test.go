package root

import (
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/interaction"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/geom"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/runtime"
)

// Single-source proof matrix for shell/body scroll visibility behavior.
func TestRoot_ScrollVisibilityProofMatrix(t *testing.T) {
	t.Parallel()

	type proofCase struct {
		name     string
		viewport geom.Rect
		run      func(t *testing.T, rt *runtime.AppLoop[state.Model], viewport geom.Rect)
		assert   []string
	}

	cases := []proofCase{
		{
			name:     "picker-focus-stays-visible-compact",
			viewport: geom.Rect{W: 80, H: 24},
			run: func(t *testing.T, rt *runtime.AppLoop[state.Model], viewport geom.Rect) {
				focusRowContaining(t, rt, viewport, "clients")
				dispatchKeys(rt, viewport, interaction.KeyEnter)
				focusRowContaining(t, rt, viewport, "client            ")
				dispatchKeys(rt, viewport, interaction.KeyEnter)
				dispatchKeys(rt, viewport,
					interaction.KeyDown,
					interaction.KeyDown,
					interaction.KeyDown,
					interaction.KeyDown,
					interaction.KeyDown,
				)
			},
			assert: []string{
				">     Other (Cline, Roo Code, OpenClaw, etc)",
			},
		},
		{
			name:     "long-payload-keeps-shell-rails-visible",
			viewport: geom.Rect{W: 80, H: 24},
			run: func(t *testing.T, rt *runtime.AppLoop[state.Model], viewport geom.Rect) {
				focusRowContaining(t, rt, viewport, "clients")
				dispatchKeys(rt, viewport, interaction.KeyEnter)
				focusRowContaining(t, rt, viewport, "client            ")
				dispatchKeys(rt, viewport, interaction.KeyEnter)
				dispatchKeys(rt, viewport,
					interaction.KeyDown,
					interaction.KeyDown,
					interaction.KeyDown,
					interaction.KeyDown,
				)
				dispatchKeys(rt, viewport, interaction.KeyEnter)
				focusRowContaining(t, rt, viewport, "file config")
				dispatchKeys(rt, viewport, interaction.KeyEnter)
			},
			assert: []string{
				"Swobu! 🧌",
				"↑↓ move",
				"tab tabs",
				"↓ more",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rt := newTestRuntime(state.Model{
				HeaderStatus:    "ready",
				DaemonState:     "up",
				Endpoints:       []string{"test"},
				CurrentEndpoint: "test",
				EndpointSnapshots: []state.EndpointSnapshot{
					{Name: "test"},
				},
			})
			rt.Rebuild(Root(), tc.viewport)
			tc.run(t, rt, tc.viewport)

			out := rt.Render(tc.viewport).String()
			for _, needle := range tc.assert {
				if !strings.Contains(out, needle) {
					t.Fatalf("render missing %q; render=%q", needle, out)
				}
			}
		})
	}
}

func dispatchKeys(rt *runtime.AppLoop[state.Model], viewport geom.Rect, keys ...interaction.Key) {
	for _, key := range keys {
		rt.DispatchEvent(updateKey(key))
		rt.Rebuild(Root(), viewport)
	}
}
