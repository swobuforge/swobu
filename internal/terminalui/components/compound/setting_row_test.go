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

func testCaster(e struct{}) update.Action {
	return update.TypedAction[struct{}]{Event: e}
}

func paintTree(t *testing.T, node *layout.LayoutNode, p paint.Painter, ctx *layout.PaintContext) {
	if node == nil || node.BorderRect.Empty() {
		return
	}
	scoped := p.WithClip(node.ClipRect).WithOrigin(geom.Point{X: node.BorderRect.X, Y: node.BorderRect.Y})
	node.RenderNode.Paint(scoped, node, ctx)
	for _, child := range node.Kids {
		paintTree(t, child, scoped, ctx)
	}
}

func TestSettingRowRendersAndEmitsCoreSignal(t *testing.T) {
	t.Parallel()

	node := SettingRow(SettingRowProps[struct{}]{
		Key:         core.K("help/ask-question"),
		Label:       "ask question",
		Value:       "",
		ActionLabel: "open ↵",
		Signal:      core.SignalEvent[struct{}]{Kind: "cockpit.help.open", Event: struct{}{}},
		Help:        []core.HelpBindingSpec{{Key: "↵", Label: "open"}},
	})

	if diags := core.Validate(node); len(diags) != 0 {
		t.Fatalf("validate diagnostics = %#v, want none", diags)
	}

	lowered, err := corelower.Lower(node, corelower.EnvConfig{}, testCaster)
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
	typedAction, ok := actions[0].(update.TypedAction[struct{}])
	if !ok {
		t.Fatalf("action = %T, want update.TypedAction[struct{}]", actions[0])
	}
	_ = typedAction

	bounds := geom.Rect{W: 48, H: 1}
	tree := (&layout.TreeBuilder{}).Build(lowered, bounds)
	buf := paint.NewBuffer(bounds)
	paintTree(t, tree, buf, &layout.PaintContext{})
	out := strings.TrimSpace(buf.String()) // swobu:io-string source=domain
	t.Logf("Output: %q", out)
	if !strings.Contains(out, "ask question") {
		t.Fatalf("render = %q, want label text", out)
	}
	if !strings.Contains(out, "open ↵") {
		t.Fatalf("render = %q, want action text", out)
	}
}

func TestSettingRowEmitsFocusAndActivationSignals(t *testing.T) {
	t.Parallel()

	node := SettingRow(SettingRowProps[struct{}]{
		Key:         core.K("row/focus"),
		Label:       "delete workspace",
		Value:       "",
		ActionLabel: "delete ↵",
		Signal:      core.SignalEvent[struct{}]{Kind: "cockpit.row.delete", Event: struct{}{}},
		FocusSignal: core.SignalEvent[struct{}]{Kind: "cockpit.row.focus", Event: struct{}{}},
		Help:        []core.HelpBindingSpec{{Key: "↵", Label: "delete"}},
	})

	interactionValue := node.InteractionValue()
	if got := len(interactionValue.FocusSignals); got != 1 {
		t.Fatalf("focus signal count = %d, want 1", got)
	}
	if got := interactionValue.FocusSignals[0].Kind; got != "cockpit.row.focus" {
		t.Fatalf("focus signal kind = %q, want cockpit.row.focus", got)
	}

	lowered, err := corelower.Lower(node, corelower.EnvConfig{}, testCaster)
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
	typedAction, ok := actions[0].(update.TypedAction[struct{}])
	if !ok {
		t.Fatalf("focus action = %T, want update.TypedAction[struct{}]", actions[0])
	}
	_ = typedAction.Event

	handler, ok := lowered.(interaction.EventHandler)
	if !ok {
		t.Fatalf("lowered type = %T, want interaction.EventHandler", lowered)
	}
	enterActions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, nil)
	if len(enterActions) != 1 {
		t.Fatalf("enter action count = %d, want 1", len(enterActions))
	}
	enterTypedAction, ok := enterActions[0].(update.TypedAction[struct{}])
	if !ok {
		t.Fatalf("enter action = %T, want update.TypedAction[struct{}]", enterActions[0])
	}
	_ = enterTypedAction
}
