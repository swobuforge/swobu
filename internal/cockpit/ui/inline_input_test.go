package ui

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func renderElementTrimmed(t *testing.T, el *tui.Element, width, height int) string {
	t.Helper()
	return testkit.RenderTrimmed(el, width, height)
}

func TestInlineInput_RenderShowsText(t *testing.T) {
	value := tui.NewState("hello")
	inp := NewInlineInput(value)

	rendered := renderElementTrimmed(t, inp.Render(), 20, 1)
	if !strings.Contains(rendered, "hello_") {
		t.Fatalf("expected 'hello_' in rendered, got %q", rendered)
	}
}

func TestInlineInput_SensitiveRenderingMasksValueButPreservesInput(t *testing.T) {
	value := tui.NewState("secret")
	inp := NewInlineInput(value)
	inp.Sensitive = true

	rendered := renderElementTrimmed(t, inp.Render(), 20, 1)
	if strings.Contains(rendered, "secret") || !strings.Contains(rendered, "••••••_") {
		t.Fatalf("sensitive render = %q", rendered)
	}
	if got := value.Get(); got != "secret" {
		t.Fatalf("value = %q, want unmodified input", got)
	}
}

func TestInlineInput_TypingInserts(t *testing.T) {
	value := tui.NewState("")
	inp := NewInlineInput(value)

	inp.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'x'})
	inp.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'y'})

	if got := inp.Value.Get(); got != "xy" {
		t.Fatalf("text = %q, want xy", got)
	}
	if got := value.Get(); got != "xy" {
		t.Fatalf("value state = %q, want xy", got)
	}
}

func TestInlineInput_BackspaceRemoves(t *testing.T) {
	value := tui.NewState("abc")
	inp := NewInlineInput(value)

	inp.HandleKey(tui.KeyEvent{Key: tui.KeyBackspace})

	if got := inp.Value.Get(); got != "ab" {
		t.Fatalf("text = %q, want ab", got)
	}
}

func TestInlineInput_LeftRightMovesCursor(t *testing.T) {
	value := tui.NewState("abc")
	inp := NewInlineInput(value)

	inp.HandleKey(tui.KeyEvent{Key: tui.KeyLeft})
	if pos := inp.CursorPos(); pos != 2 {
		t.Fatalf("cursor after Left = %d, want 2", pos)
	}
	inp.HandleKey(tui.KeyEvent{Key: tui.KeyLeft})
	inp.HandleKey(tui.KeyEvent{Key: tui.KeyLeft})
	if pos := inp.CursorPos(); pos != 0 {
		t.Fatalf("cursor at start = %d, want 0", pos)
	}
	inp.HandleKey(tui.KeyEvent{Key: tui.KeyLeft}) // clamped
	if pos := inp.CursorPos(); pos != 0 {
		t.Fatalf("cursor clamped = %d, want 0", pos)
	}
}

func TestInlineInput_EnterCallsOnSubmit(t *testing.T) {
	var submitted string
	value := tui.NewState("dev")
	inp := NewInlineInput(value)
	inp.OnSubmit = func(s string) {
		submitted = s
	}

	inp.HandleKey(tui.KeyEvent{Key: tui.KeyEnter})

	if submitted != "dev" {
		t.Fatalf("submitted = %q, want dev", submitted)
	}
}

func TestInlineInput_EscapeNotConsumed(t *testing.T) {
	value := tui.NewState("x")
	inp := NewInlineInput(value)

	if consumed := inp.HandleKey(tui.KeyEvent{Key: tui.KeyEscape}); consumed {
		t.Fatal("Escape should not be consumed by InlineInput")
	}
}

func TestInlineInput_BlinkAffectsCursorRune(t *testing.T) {
	value := tui.NewState("a")
	inp := NewInlineInput(value)
	inp.blink.Set(true)

	renderedOn := renderElementTrimmed(t, inp.Render(), 10, 1)
	if !strings.Contains(renderedOn, "_") {
		t.Fatalf("expected cursor '_' when blink=true, got %q", renderedOn)
	}

	inp.blink.Set(false)
	renderedOff := renderElementTrimmed(t, inp.Render(), 10, 1)
	if strings.Contains(renderedOff, "_") {
		t.Fatalf("expected no cursor when blink=false, got %q", renderedOff)
	}
}

func TestInlineInput_WatchersNonEmpty(t *testing.T) {
	value := tui.NewState("")
	inp := NewInlineInput(value)
	if len(inp.Watchers()) == 0 {
		t.Fatal("expected at least one blink watcher")
	}
}

func TestInlineInput_ScrollBackspaceWithinViewport(t *testing.T) {
	// Create a value longer than the default width so scrolling is relevant.
	longText := strings.Repeat("x", 50)
	value := tui.NewState(longText)
	inp := NewInlineInput(value)
	// Move cursor to end.
	inp.moveEnd()
	// Scroll should have moved to keep the cursor visible.
	if inp.scrollPos.Get() <= 0 {
		t.Fatalf("scroll should have advanced for long text, got %d", inp.scrollPos.Get())
	}
	scrollBefore := inp.scrollPos.Get()
	inp.backspace()
	// After backspace the scroll may adjust.
	if inp.scrollPos.Get() > scrollBefore+1 {
		t.Fatalf("scroll jumped unexpectedly from %d to %d", scrollBefore, inp.scrollPos.Get())
	}
}
