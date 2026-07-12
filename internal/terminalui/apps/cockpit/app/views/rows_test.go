package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/reconcile"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func TestSettingActionRowEmitsCockpitActionAndFocusSignals(t *testing.T) {
	t.Parallel()

	spec := SettingActionRow(
		"help/ask-question",
		"ask question",
		"",
		"open",
		state.OpenSupportLinkRequested{Label: "ask question", URL: "https://x.com/ml_review"},
		false,
	)
	tree := renderRowSpec(t, spec)

	buf := paint.NewBuffer(geom.Rect{W: 80, H: 1})
	paintLayoutTree(tree, buf, &layout.PaintContext{FocusedID: tree.ID}, geom.Point{})
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "ask question") {
		t.Fatalf("render = %q, want label text", out)
	}
	if !strings.Contains(out, "open ↵") {
		t.Fatalf("render = %q, want open action label", out)
	}

	focusEvents, ok := tree.RenderNode.(interaction.FocusEvents)
	if !ok {
		t.Fatalf("type = %T, want interaction.FocusEvents", tree.RenderNode)
	}
	focusActions := focusEvents.OnFocus(tree)
	if len(focusActions) != 1 {
		t.Fatalf("focus action count = %d, want 1", len(focusActions))
	}
	focusSignal, ok := focusActions[0].(update.CoreSignalAction)
	if !ok {
		t.Fatalf("focus action type = %T, want update.CoreSignalAction", focusActions[0])
	}
	if got := focusSignal.Signal.Kind; got != cockpitRowFocusSignalKind {
		t.Fatalf("focus signal kind = %q, want %q", got, cockpitRowFocusSignalKind)
	}
	focusPayload, ok := focusSignal.Signal.Data.(state.SetFocusedRowAffordance)
	if !ok {
		t.Fatalf("focus payload type = %T, want state.SetFocusedRowAffordance", focusSignal.Signal.Data)
	}
	if focusPayload.Verb != "open" || focusPayload.AllowSpace {
		t.Fatalf("focus payload = %#v, want verb=open allowSpace=false", focusPayload)
	}

	handler, ok := tree.RenderNode.(interaction.EventHandler)
	if !ok {
		t.Fatalf("type = %T, want interaction.EventHandler", tree.RenderNode)
	}
	actions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, tree)
	if len(actions) != 1 {
		t.Fatalf("action count = %d, want 1", len(actions))
	}
	coreSignal, ok := actions[0].(update.CoreSignalAction)
	if !ok {
		t.Fatalf("action type = %T, want update.CoreSignalAction", actions[0])
	}
	if got := coreSignal.Signal.Kind; got != cockpitActionSignalKind {
		t.Fatalf("signal kind = %q, want %q", got, cockpitActionSignalKind)
	}
	openPayload, ok := coreSignal.Signal.Data.(state.OpenSupportLinkRequested)
	if !ok {
		t.Fatalf("payload type = %T, want state.OpenSupportLinkRequested", coreSignal.Signal.Data)
	}
	if openPayload.Label != "ask question" {
		t.Fatalf("payload label = %q, want ask question", openPayload.Label)
	}
}

func TestSettingStaticRowDoesNotFocus(t *testing.T) {
	t.Parallel()

	tree := renderRowSpec(t, SettingStaticRow("delete workspace", ""))

	buf := paint.NewBuffer(geom.Rect{W: 48, H: 1})
	paintLayoutTree(tree, buf, &layout.PaintContext{FocusedID: tree.ID}, geom.Point{})
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "delete workspace") {
		t.Fatalf("render = %q, want static label", out)
	}

	focusable, ok := tree.RenderNode.(interaction.Focusable)
	if !ok {
		t.Fatalf("type = %T, want interaction.Focusable", tree.RenderNode)
	}
	if focusable.CanFocus(tree) {
		t.Fatal("static row should not be focusable")
	}
}

func renderRowSpec(t *testing.T, spec retained.ViewSpec[state.Model]) *layout.LayoutNode {
	t.Helper()

	ctx := &retained.Context[state.Model]{
		Local: reconcile.NewLocalStore().Scope(1),
		Model: func() state.Model { return state.Model{} },
	}
	node := retained.Materialize(ctx, spec)
	tree := (&layout.TreeBuilder{}).Build(node, geom.Rect{W: 80, H: 1})
	if tree == nil {
		t.Fatal("layout tree is nil")
	}
	return tree
}
