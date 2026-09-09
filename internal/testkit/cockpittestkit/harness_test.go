package testkit

import (
	"strings"
	"testing"

	"github.com/grindlemire/go-tui"
)

func TestHarness_CanConstructFromComponent(t *testing.T) {
	// Use a simple struct component that satisfies tui.Component
	root := &simpleComp{el: tui.New(tui.WithText("hello"))}

	h, err := NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	got := h.Frame()
	if !strings.Contains(got, "hello") {
		t.Fatalf("frame missing 'hello', got:\n%s", got)
	}
}

type simpleComp struct {
	el *tui.Element
}

func (s *simpleComp) Render(app *tui.App) *tui.Element {
	return s.el
}

func TestHarness_CanConstructFromElement(t *testing.T) {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
	)
	root.AddChild(tui.New(tui.WithText("A")))
	root.AddChild(tui.New(tui.WithText("B")))

	h, err := NewFuncHarness(root)
	if err != nil {
		t.Fatalf("NewFuncHarness: %v", err)
	}
	defer h.Close()

	got := h.Frame()
	if !strings.Contains(got, "A") || !strings.Contains(got, "B") {
		t.Fatalf("frame missing expected content: %q", got)
	}
}

func TestHarness_FocusNextRenders(t *testing.T) {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
	)
	root.AddChild(tui.New(
		tui.WithText("A"),
		tui.WithOnFocus(func(*tui.Element) {}),
	))
	root.AddChild(tui.New(
		tui.WithText("B"),
		tui.WithOnFocus(func(*tui.Element) {}),
	))

	h, err := NewFuncHarness(root)
	if err != nil {
		t.Fatalf("NewFuncHarness: %v", err)
	}
	defer h.Close()

	h.App().FocusNext()
	got := h.Frame()
	if !strings.Contains(got, "A") || !strings.Contains(got, "B") {
		t.Fatalf("frame missing expected content after focus: %q", got)
	}
}
