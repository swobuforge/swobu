package corelower

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

func TestLowerInputFocusedEmptyKeepsCaretVisible(t *testing.T) {
	t.Parallel()

	tree := lowerInputTree(t, "")
	assertPaint(t, tree, "> _")
}

func TestLowerInputRebuildUsesCallerOwnedValue(t *testing.T) {
	t.Parallel()

	assertPaint(t, lowerInputTree(t, "ac"), "> ac_")
	assertPaint(t, lowerInputTree(t, "xyz"), "> xyz_")
}

func TestLowerInputHandlesEditingKeysWithoutMutatingValue(t *testing.T) {
	t.Parallel()

	tree := lowerInputTree(t, "ac")
	handler, ok := tree.RenderNode.(interaction.ScopedEventHandler)
	if !ok {
		t.Fatalf("type = %T, want interaction.ScopedEventHandler", tree.RenderNode)
	}
	focusable, ok := tree.RenderNode.(interaction.Focusable)
	if !ok {
		t.Fatalf("type = %T, want interaction.Focusable", tree.RenderNode)
	}
	if !focusable.CanFocus(tree) {
		t.Fatal("input should be focusable")
	}

	assertPaint(t, tree, "> ac_")

	cases := []struct {
		name string
		ev   interaction.Event
	}{
		{
			name: "rune",
			ev:   interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyRune, Rune: 'x'},
		},
		{
			name: "backspace",
			ev:   interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyBackspace},
		},
		{
			name: "enter",
			ev:   interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter},
		},
		{
			name: "esc",
			ev:   interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEsc},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handled, actions := handler.HandleScopedEvent(tc.ev, tree)
			if !handled {
				t.Fatalf("handled = %v, want true", handled)
			}
			if len(actions) != 1 {
				t.Fatalf("actions len = %d, want 1", len(actions))
			}

			assertPaint(t, tree, "> ac_")
		})
	}
}

func lowerInputTree(t *testing.T, value string) *layout.LayoutNode {
	t.Helper()

	node := core.Input[struct{}](value).Interaction(core.InteractionSpec[struct{}]{
		Focus: core.FocusSpec{Mode: core.Focusable},
		Keymap: []core.KeyBindingSpec{
			{Pattern: core.KeyEnter(), Intent: core.IntentActivate},
			{Pattern: core.KeyEsc(), Intent: core.IntentCancel},
			{Pattern: core.KeyMatch{Name: "backspace"}, Intent: core.IntentEdit},
			{Pattern: core.KeyRune('x'), Intent: core.IntentEdit},
		},
		Signals: []core.SignalEvent[struct{}]{
			{Kind: "input.change"},
			{Kind: "input.commit"},
			{Kind: "input.cancel"},
		},
	})
	renderNode, err := Lower(node, EnvConfig{}, testCaster)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 16, H: 1})
	if tree == nil {
		t.Fatal("layout tree is nil")
	}
	return tree
}

func assertPaint(t *testing.T, tree *layout.LayoutNode, want string) {
	t.Helper()

	buf := paint.NewBuffer(geom.Rect{W: 16, H: 1})
	paintNode(tree, buf, &layout.PaintContext{FocusedID: tree.ID})
	if got := strings.TrimRight(buf.String(), " "); got != want {
		t.Fatalf("paint = %q, want %q", got, want)
	}
}
