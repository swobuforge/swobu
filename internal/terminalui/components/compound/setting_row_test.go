package compound

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestSettingRowRendersAndEmitsCoreSignal(t *testing.T) {
	t.Parallel()

	node := SettingRow(SettingRowProps{
		Key:         core.K("help/ask-question"),
		Label:       "ask question",
		Value:       "",
		ActionLabel: "open ↵",
		Signal:      core.SignalEvent{Kind: "cockpit.help.open", Data: struct{}{}},
		Help:        []core.HelpBindingSpec{{Key: "↵", Label: "open"}},
	})

	if diags := core.Validate(node); len(diags) != 0 {
		t.Fatalf("validate diagnostics = %#v, want none", diags)
	}

	lowered, err := corelower.Lower(node, corelower.EnvConfig{})
	if err != nil {
		t.Fatalf("lower setting row: %v", err)
	}

	handler, ok := lowered.(interaction.EventHandler)
	if !ok {
		t.Fatalf("lowered type = %T, want interaction.EventHandler", lowered)
	}
	actions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, nil)
	if len(actions) != 1 {
		t.Fatalf("action count = %d, want 1", len(actions))
	}
	signal, ok := actions[0].(update.CoreSignalAction)
	if !ok {
		t.Fatalf("action = %T, want update.CoreSignalAction", actions[0])
	}
	if got := signal.Signal.Kind; got != "cockpit.help.open" {
		t.Fatalf("signal kind = %q, want cockpit.help.open", got)
	}

	buf := paint.NewBuffer(geom.Rect{W: 48, H: 1})
	lowered.Paint(buf, &layout.LayoutNode{BorderRect: geom.Rect{W: 48, H: 1}}, &layout.PaintContext{})
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "ask question") {
		t.Fatalf("render = %q, want label text", out)
	}
	if !strings.Contains(out, "open ↵") {
		t.Fatalf("render = %q, want action text", out)
	}
}

func TestSettingRowEmitsFocusAndActivationSignals(t *testing.T) {
	t.Parallel()

	node := SettingRow(SettingRowProps{
		Key:         core.K("row/focus"),
		Label:       "delete workspace",
		Value:       "",
		ActionLabel: "delete ↵",
		Signal:      core.SignalEvent{Kind: "cockpit.row.delete", Data: struct{}{}},
		FocusSignal: core.SignalEvent{Kind: "cockpit.row.focus", Data: "delete"},
		Help:        []core.HelpBindingSpec{{Key: "↵", Label: "delete"}},
	})

	interactionValue := node.InteractionValue()
	if got := len(interactionValue.FocusSignals); got != 1 {
		t.Fatalf("focus signal count = %d, want 1", got)
	}
	if got := interactionValue.FocusSignals[0].Kind; got != "cockpit.row.focus" {
		t.Fatalf("focus signal kind = %q, want cockpit.row.focus", got)
	}

	lowered, err := corelower.Lower(node, corelower.EnvConfig{})
	if err != nil {
		t.Fatalf("lower setting row: %v", err)
	}

	focusEvents, ok := lowered.(interaction.FocusEvents)
	if !ok {
		t.Fatalf("lowered type = %T, want interaction.FocusEvents", lowered)
	}
	actions := focusEvents.OnFocus(nil)
	if len(actions) != 1 {
		t.Fatalf("focus action count = %d, want 1", len(actions))
	}
	signal, ok := actions[0].(update.CoreSignalAction)
	if !ok {
		t.Fatalf("focus action = %T, want update.CoreSignalAction", actions[0])
	}
	if got := signal.Signal.Kind; got != "cockpit.row.focus" {
		t.Fatalf("focus signal kind = %q, want cockpit.row.focus", got)
	}
	if got := signal.Signal.Data.(string); got != "delete" {
		t.Fatalf("focus signal data = %q, want delete", got)
	}

	handler, ok := lowered.(interaction.EventHandler)
	if !ok {
		t.Fatalf("lowered type = %T, want interaction.EventHandler", lowered)
	}
	enterActions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, nil)
	if len(enterActions) != 1 {
		t.Fatalf("enter action count = %d, want 1", len(enterActions))
	}
	enterSignal, ok := enterActions[0].(update.CoreSignalAction)
	if !ok {
		t.Fatalf("enter action = %T, want update.CoreSignalAction", enterActions[0])
	}
	if got := enterSignal.Signal.Kind; got != "cockpit.row.delete" {
		t.Fatalf("enter signal kind = %q, want cockpit.row.delete", got)
	}
}
