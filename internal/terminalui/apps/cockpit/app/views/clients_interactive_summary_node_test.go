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

func TestBuildClientsInteractiveSummaryNode_ReturnsCoreNode(t *testing.T) {
	t.Parallel()

	node := BuildClientsInteractiveSummaryNode("  Claude  ")
	if diags := core.Validate(node); len(diags) > 0 {
		t.Fatalf("validation failed: %v", diags)
	}

	interactionValue := node.InteractionValue()
	if got := len(interactionValue.Signals); got != 1 {
		t.Fatalf("activation signal count = %d, want 1", got)
	}
	activationSignal := interactionValue.Signals[0]
	if got, want := activationSignal.Kind, cockpitActionSignalKind; got != want {
		t.Fatalf("activation signal kind = %q, want %q", got, want)
	}
	activationEvent, ok := activationSignal.Event.(state.SetInteractionMode)
	if !ok {
		t.Fatalf("activation signal event = %T, want state.SetInteractionMode", activationSignal.Event)
	}
	if activationEvent.Mode != state.InteractionModePickOne {
		t.Fatalf("activation signal mode = %q, want %q", activationEvent.Mode, state.InteractionModePickOne)
	}
	if got := len(interactionValue.FocusSignals); got != 1 {
		t.Fatalf("focus signal count = %d, want 1", got)
	}
	focusSignal := interactionValue.FocusSignals[0]
	if got, want := focusSignal.Kind, cockpitRowFocusSignalKind; got != want {
		t.Fatalf("focus signal kind = %q, want %q", got, want)
	}
	focusEvent, ok := focusSignal.Event.(state.SetFocusedRowAffordance)
	if !ok {
		t.Fatalf("focus signal event = %T, want state.SetFocusedRowAffordance", focusSignal.Event)
	}
	if focusEvent.Verb != "choose" {
		t.Fatalf("focus signal verb = %q, want choose", focusEvent.Verb)
	}
	if focusEvent.AllowSpace {
		t.Fatal("focus signal should not allow space")
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

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 80, H: 2})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 2})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String())

	for _, want := range []string{"client", "Claude", "choose"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render = %q, want %q", out, want)
		}
	}
}
