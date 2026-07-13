package corelower

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestLowerTextPaintsText(t *testing.T) {
	t.Parallel()

	renderNode, err := Lower(core.Text("hello"), EnvConfig{})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 16, H: 2})
	buf := paint.NewBuffer(geom.Rect{W: 16, H: 2})
	paintNode(tree, buf, &layout.PaintContext{})
	if got := buf.String(); got != "hello" {
		t.Fatalf("paint = %q, want hello", got)
	}
}

func TestLowerStackLowersChildrenInOrder(t *testing.T) {
	t.Parallel()

	renderNode, err := Lower(core.Stack(core.AxisVertical, core.Text("a"), core.Text("b")), EnvConfig{})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 8, H: 4})
	buf := paint.NewBuffer(geom.Rect{W: 8, H: 4})
	paintNode(tree, buf, &layout.PaintContext{})
	if got := buf.String(); got != "a\nb" {
		t.Fatalf("paint = %q, want a\\nb", got)
	}
}

func TestLowerAssertRejectsInvalidNode(t *testing.T) {
	t.Parallel()

	node := core.Action("open", core.SignalEvent{Kind: "opened"}).
		Interaction(core.InteractionSpec{
			Focus:  core.FocusSpec{Mode: core.Focusable},
			Keymap: []core.KeyBindingSpec{{Pattern: core.KeyEnter(), Intent: core.IntentActivate}},
		})

	_, err := LowerAssert(node, EnvConfig{})
	if err == nil {
		t.Fatal("expected LowerAssert to reject action without signal")
	}
	if !strings.Contains(err.Error(), "action without emitted event") {
		t.Fatalf("error = %q, want action without emitted event", err.Error())
	}
}

func TestLowerAllowsInvalidNodeAtRuntime(t *testing.T) {
	t.Parallel()

	renderNode, err := Lower(core.Box(
		core.Text("a").Key(core.K("dup")),
		core.Text("b").Key(core.K("dup")),
	), EnvConfig{})
	if err != nil {
		t.Fatalf("unexpected lowering failure: %v", err)
	}
	if renderNode == nil {
		t.Fatal("expected lowered render node")
	}
}

func TestLowerActionIsFocusableAndEmitsSignal(t *testing.T) {
	t.Parallel()

	renderNode, err := Lower(core.Action("open", core.SignalEvent{Kind: "opened"}), EnvConfig{})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 16, H: 1})
	if _, ok := tree.RenderNode.(interaction.Focusable); !ok {
		t.Fatalf("type = %T, want focusable", tree.RenderNode)
	}
	handler, ok := tree.RenderNode.(interaction.EventHandler)
	if !ok {
		t.Fatalf("type = %T, want event handler", tree.RenderNode)
	}
	actions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, tree)
	if len(actions) != 1 {
		t.Fatalf("actions len = %d, want 1", len(actions))
	}
	if _, ok := actions[0].(update.CoreSignalAction); !ok {
		t.Fatalf("action type = %T, want update.CoreSignalAction", actions[0])
	}

	buf := paint.NewBuffer(geom.Rect{W: 16, H: 1})
	paintNode(tree, buf, &layout.PaintContext{FocusedID: tree.ID})
	if got := buf.String(); !strings.Contains(got, "> open") {
		t.Fatalf("focused paint = %q, want focus marker", got)
	}
}

func TestLowerActionWithFocusSignalDoesNotHandleEnter(t *testing.T) {
	t.Parallel()

	node := core.Action("delete", core.SignalEvent{}).
		Key(core.K("workspace/delete")).
		Interaction(core.InteractionSpec{
			Focus:  core.FocusSpec{Mode: core.Focusable},
			Keymap: []core.KeyBindingSpec{{Pattern: core.KeyEnter(), Intent: core.IntentActivate}},
			Help:   []core.HelpBindingSpec{{Key: "enter", Label: "delete"}},
			FocusSignals: []core.SignalEvent{{
				Kind: "cockpit.row.focus",
				Data: "delete",
			}},
		}).
		Contract(core.Contract{
			Name:    "Action",
			Purpose: "Focusable semantic action.",
			Help:    []core.HelpBindingSpec{{Key: "enter", Label: "delete"}},
			Focus:   core.FocusPolicy{FocusableWhenEnabled: true},
			Layout:  core.LayoutPolicy{Width: core.Fill(1), Height: core.Fit()},
		})

	renderNode, err := Lower(node, EnvConfig{})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 16, H: 1})
	focusEvents, ok := tree.RenderNode.(interaction.FocusEvents)
	if !ok {
		t.Fatalf("type = %T, want interaction.FocusEvents", tree.RenderNode)
	}
	actions := focusEvents.OnFocus(tree)
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

	handler, ok := tree.RenderNode.(interaction.EventHandler)
	if !ok {
		t.Fatalf("type = %T, want interaction.EventHandler", tree.RenderNode)
	}
	enterActions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, tree)
	if len(enterActions) != 0 {
		t.Fatalf("enter action count = %d, want 0", len(enterActions))
	}
}

func paintNode(node *layout.LayoutNode, painter paint.Painter, ctx *layout.PaintContext) {
	if node.ClipRect.Empty() || node.BorderRect.Empty() {
		return
	}
	scoped := painter.WithClip(node.ClipRect).WithOrigin(geom.Point{X: node.BorderRect.X, Y: node.BorderRect.Y})
	node.RenderNode.Paint(scoped, node, ctx)
	for _, child := range node.Kids {
		paintNode(child, scoped, ctx)
	}
}
