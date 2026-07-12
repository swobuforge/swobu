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

func TestTextMutedAndStacksExposeSemanticTokens(t *testing.T) {
	t.Parallel()

	text := Text("hello")
	if got := text.ContentValue().Text; got != "hello" {
		t.Fatalf("text content = %q, want hello", got)
	}

	muted := Muted("quiet")
	if got := muted.StyleValue().Token; got != core.TokenTextMuted {
		t.Fatalf("muted token = %q, want %q", got, core.TokenTextMuted)
	}

	vertical := VStack(Text("a"), Text("b"))
	if got := vertical.LayoutValue().Flow.Axis; got != core.AxisVertical {
		t.Fatalf("vstack axis = %v, want vertical", got)
	}

	horizontal := HStack(Text("a"), Text("b"))
	if got := horizontal.LayoutValue().Flow.Axis; got != core.AxisHorizontal {
		t.Fatalf("hstack axis = %v, want horizontal", got)
	}
}

func TestActionEmitsSignalAndHonorsDisabledFocus(t *testing.T) {
	t.Parallel()

	node := Action(ActionProps{
		Key:    core.K("help.ask"),
		Label:  "ask question  open ↵",
		Signal: core.Signal{Kind: "cockpit.help.open", Data: struct{}{}},
	})
	lowered, err := corelower.Lower(node, corelower.Env{})
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
	signal, ok := actions[0].(update.CoreSignalAction)
	if !ok {
		t.Fatalf("action = %T, want update.CoreSignalAction", actions[0])
	}
	if got := signal.Signal.Kind; got != "cockpit.help.open" {
		t.Fatalf("signal kind = %q, want cockpit.help.open", got)
	}

	disabled := Action(ActionProps{
		Key:      core.K("help.disabled"),
		Label:    "disabled",
		Signal:   core.Signal{Kind: "cockpit.help.open"},
		Disabled: true,
	})
	disabledLowered, err := corelower.Lower(disabled, corelower.Env{})
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

func TestInputSeedsSemanticSignalsAndContract(t *testing.T) {
	t.Parallel()

	node := Input(InputProps{
		Key:        core.K("env-input"),
		Label:      "env",
		Value:      "ac",
		EmptyValue: "workspace",
		OnChange:   core.Signal{Kind: "cockpit.input.change"},
		OnCommit:   core.Signal{Kind: "cockpit.input.commit"},
		OnCancel:   core.Signal{Kind: "cockpit.input.cancel"},
	})

	if got := node.Kind(); got != core.KindInput {
		t.Fatalf("kind = %v, want KindInput", got)
	}
	if got := node.KeyValue(); got != core.K("env-input") {
		t.Fatalf("key = %q, want env-input", got)
	}
	if got := node.ContentValue().Text; got != "ac" {
		t.Fatalf("content = %q, want ac", got)
	}
	if got := node.DebugValue().Name; got != "env" {
		t.Fatalf("debug name = %q, want env", got)
	}
	if got := node.InteractionValue().Focus.Mode; got != core.Focusable {
		t.Fatalf("focus mode = %v, want Focusable", got)
	}
	if got := len(node.InteractionValue().Signals); got != 3 {
		t.Fatalf("signal count = %d, want 3", got)
	}
	if got := node.InteractionValue().Signals[0].Kind; got != "cockpit.input.change" {
		t.Fatalf("change signal kind = %q, want cockpit.input.change", got)
	}
	if got := node.ContractValue().Name; got != "Input" {
		t.Fatalf("contract name = %q, want Input", got)
	}
	if got := len(node.ContractValue().Signals); got != 3 {
		t.Fatalf("contract signals = %d, want 3", got)
	}
}
