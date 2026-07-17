package routes

import (
	"context"
	"os"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestRouteDeleteRow_EscapeClosesConfirmWithoutDeleting(t *testing.T) {
	deletes := 0
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteRoute: func(context.Context, ports.DeleteRouteRequest) error {
			deletes++
			return nil
		},
	})
	row := RouteDeleteRowComponent(section, routeSectionModel().Routes[0])

	h, err := testkit.NewHarness(row)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.App().FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	frame := h.Frame()
	if !strings.Contains(frame, "delete gpt?") || !strings.Contains(frame, "confirm ↵") {
		t.Fatalf("expected delete confirmation after Enter:\n%s", frame)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if deletes != 0 {
		t.Fatalf("deletes after Escape = %d, want 0", deletes)
	}
	frame = h.Frame()
	if !strings.Contains(frame, "delete ↵") {
		t.Fatalf("expected collapsed delete row after Escape:\n%s", frame)
	}
	if strings.Contains(frame, "confirm ↵") || strings.Contains(frame, "delete gpt?") {
		t.Fatalf("Escape should close only the confirmation:\n%s", frame)
	}
}

func TestRouteDeleteRow_EscapeAfterRenderThenKeyMapClosesConfirm(t *testing.T) {
	row := RouteDeleteRowComponent(Section(routeSectionModel(), nil), routeSectionModel().Routes[0])
	row.OpenConfirm()

	row.Render(nil)
	keymap := row.KeyMap()
	dispatchBinding(t, keymap, tui.KeyEscape)

	if row.IsOpen() {
		t.Fatal("Escape after render then keymap should close confirmation")
	}
}

func TestRouteDeleteRow_EscapeAfterKeyMapThenRenderClosesConfirm(t *testing.T) {
	row := RouteDeleteRowComponent(Section(routeSectionModel(), nil), routeSectionModel().Routes[0])
	row.OpenConfirm()

	keymap := row.KeyMap()
	row.Render(nil)
	dispatchBinding(t, keymap, tui.KeyEscape)

	if row.IsOpen() {
		t.Fatal("Escape after keymap then render should close confirmation")
	}
}

func TestRouteDeleteRow_DoesNotOwnRawFocusedEscapeBinding(t *testing.T) {
	src, err := os.ReadFile("section_behavior.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(src), "tui.OnFocused"+"(tui.KeyEscape") {
		t.Fatal("RouteDeleteRow must express local Escape through ui.ActionTarget, not raw go-tui dispatch")
	}
}

func dispatchBinding(t *testing.T, keymap tui.KeyMap, key tui.Key) {
	t.Helper()
	for _, binding := range keymap {
		if binding.Pattern.Key == key {
			binding.Handler(tui.KeyEvent{Key: key})
			return
		}
	}
	t.Fatalf("keymap missing binding for %v", key)
}
