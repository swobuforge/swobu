package primitive

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func testCaster(e struct{}) update.Action {
	return update.TypedAction[struct{}]{Event: e}
}

func TestActionEmitsSignalAndHonorsDisabledFocus(t *testing.T) {
	t.Parallel()

	node := Action(ActionProps[struct{}]{
		Key:    core.K("help.ask"),
		Label:  "ask question  open ↵",
		Signal: core.SignalEvent[struct{}]{Kind: "cockpit.help.open", Event: struct{}{}},
	})
	lowered, err := corelower.Lower(node, corelower.EnvConfig{}, testCaster)
	if err != nil {
		t.Fatalf("lower action: %v", err)
	}

	focusable, ok := lowered.(interaction.Focusable)
	if !ok {
		t.Fatalf("lowered type = %T, want interaction.Focusable", lowered)
	}
	if !focusable.CanFocus(nil) {
		t.Fatal("action should be focusable when enabled")
	}

	handler, ok := lowered.(interaction.EventHandler)
	if !ok {
		t.Fatalf("lowered type = %T, want interaction.EventHandler", lowered)
	}
	actions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, nil)
	if len(actions) != 1 {
		t.Fatalf("action count = %d, want 1", len(actions))
	}
	if got := len(actions); got != 1 {
		t.Fatalf("action count = %d, want 1", got)
	}
	typedAction, ok := actions[0].(update.TypedAction[struct{}])
	if !ok {
		t.Fatalf("action = %T, want update.TypedAction[struct{}]", actions[0])
	}
	// Signal data is passed through the caster; for struct{} caster it wraps the event
	_ = typedAction

	disabled := Action(ActionProps[struct{}]{
		Key:      core.K("help.disabled"),
		Label:    "disabled",
		Signal:   core.SignalEvent[struct{}]{Kind: "cockpit.help.open"},
		Disabled: true,
	})
	disabledLowered, err := corelower.Lower(disabled, corelower.EnvConfig{}, testCaster)
	if err != nil {
		t.Fatalf("lower disabled action: %v", err)
	}
	disabledFocusable, ok := disabledLowered.(interaction.Focusable)
	if !ok {
		t.Fatalf("lowered type = %T, want interaction.Focusable", disabledLowered)
	}
	if disabledFocusable.CanFocus(nil) {
		t.Fatal("disabled action should not be focusable")
	}

	rect := geom.Rect{W: 24, H: 1}
	buf := paint.NewBuffer(rect)
	disabledLowered.Paint(buf, &layout.LayoutNode{BorderRect: rect}, &layout.PaintContext{})
	if got := buf.String(); got != "disabled" {
		t.Fatalf("disabled paint = %q, want disabled", got)
	}
}
