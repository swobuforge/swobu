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

func TestBuildMismatchScreenNode_ReturnsCoreNode(t *testing.T) {
	t.Parallel()

	model := state.Model{
		HeaderStatus: "ready",
		DaemonState:  "up",
		ControlPlane: &state.ControlPlaneMismatch{
			ExpectedProtocol:  7,
			DaemonProtocol:    6,
			HasDaemonProtocol: true,
			TUIVersion:        "1.0.0",
			DaemonVersion:     "0.9.0",
			Reason:            "protocol mismatch",
			RecoveryCommand:   "restart daemon",
		},
	}

	node := BuildMismatchScreenNode(model)
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

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 80, H: 24})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 24})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String())

	if !strings.Contains(out, "TUI and daemon are incompatible") {
		t.Fatalf("render = %q, want status row", out)
	}
	if !strings.Contains(out, "swobu 1.0.0") {
		t.Fatalf("render = %q, want tui version", out)
	}
	if !strings.Contains(out, "restart daemon") {
		t.Fatalf("render = %q, want restart action", out)
	}
	if !strings.Contains(out, "copy diagnostics") {
		t.Fatalf("render = %q, want copy diagnostics action", out)
	}
}
