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
		name     string
		ev       interaction.Event
		wantKind string
		wantData string
	}{
		{
			name:     "rune",
			ev:       interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyRune, Rune: 'x'},
			wantKind: "input.change",
			wantData: "acx",
		},
		{
			name:     "backspace",
			ev:       interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyBackspace},
			wantKind: "input.change",
			wantData: "a",
		},
		{
			name:     "enter",
			ev:       interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter},
			wantKind: "input.commit",
			wantData: "ac",
		},
		{
			name:     "esc",
			ev:       interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEsc},
			wantKind: "input.cancel",
			wantData: "keep",
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
			signal, ok := actions[0].(update.CoreSignalAction)
			if !ok {
				t.Fatalf("action type = %T, want update.CoreSignalAction", actions[0])
			}
			if got := signal.Signal.Kind; got != tc.wantKind {
				t.Fatalf("signal kind = %q, want %q", got, tc.wantKind)
			}
			got, ok := signal.Signal.Data.(string)
			if !ok {
				t.Fatalf("signal data type = %T, want string", signal.Signal.Data)
			}
			if got != tc.wantData {
				t.Fatalf("signal data = %q, want %q", got, tc.wantData)
			}

			assertPaint(t, tree, "> ac_")
		})
	}
}

func lowerInputTree(t *testing.T, value string) *layout.LayoutNode {
	t.Helper()

	node := core.Input(value).Interaction(core.Interaction{
		Focus: core.FocusSpec{Mode: core.Focusable},
		Signals: []core.Signal{
			{Kind: "input.change"},
			{Kind: "input.commit"},
			{Kind: "input.cancel", Data: "keep"},
		},
	})
	renderNode, err := Lower(node, Env{})
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
