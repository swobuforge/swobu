package routes

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestRouteSection_FocusTraversal(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.State.ExpandedRoute.Set(section.State.Routes[0].ID)
	h := makeHarness(t, section)
	defer h.Close()

	// All selectable rows should be in focus tree
	root := h.App().Root()
	if root == nil {
		t.Fatal("app.Root() returned nil")
	}
	focusables := collectFocusables(root)
	if got, want := len(focusables), 6; got != want {
		t.Fatalf("expanded route focusables = %d, want %d", got, want)
	}
}

func TestRouteSection_FocusMarkersRendered(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.State.ExpandedRoute.Set(section.State.Routes[0].ID)
	h := makeHarness(t, section)
	defer h.Close()

	h.FocusNext()

	frame := h.Frame()
	if !strings.Contains(frame, "> ") {
		t.Fatalf("no focused row marker found in frame:\n%s", frame)
	}
}

func TestRouteSection_FocusMoveUpdatesMarker(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.State.ExpandedRoute.Set(section.State.Routes[0].ID)
	h := makeHarness(t, section)
	defer h.Close()

	h.FocusNext()

	frame1 := h.Frame()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	frame2 := h.Frame()
	if frame1 == frame2 {
		t.Fatal("frames identical after focus move; expected different > row")
	}
}

func TestRouteSection_EnterActivatesRow(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.State.ExpandedRoute.Set(section.State.Routes[0].ID)
	h := makeHarness(t, section)
	defer h.Close()

	// Focus the first row (route row)
	h.FocusNext()

	frameBefore := h.Frame()
	if !strings.Contains(frameBefore, "> gpt") {
		t.Fatalf("expected gpt route focused before Enter, got:\n%s", frameBefore)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if section.State.ExpandedRoute.Get() != "" {
		t.Fatalf("expected route toggled closed after activation, got %q", section.State.ExpandedRoute.Get())
	}

	frameAfter := h.Frame()
	if !strings.Contains(frameAfter, "> ") {
		t.Fatalf("expected a focused row after Enter, got:\n%s", frameAfter)
	}
}

func TestRouteSection_SpaceActivatesRow(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.State.ExpandedRoute.Set(section.State.Routes[0].ID)
	h := makeHarness(t, section)
	defer h.Close()

	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: ' '})

	if section.State.ExpandedRoute.Get() != "" {
		t.Fatalf("expected route toggled closed after Space, got %q", section.State.ExpandedRoute.Get())
	}
}

func TestRouteAdd_InputSubmitCreatesDraftRoute(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.addRoute()
	h := makeHarness(t, section)
	defer h.Close()

	h.Frame()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'x'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if got := section.State.ExpandedRoute.Get(); got != "route-newx" {
		t.Fatalf("expanded route = %q, want route-newx", got)
	}
	if got := section.State.AddTargetRoute.Get(); got != "route-newx" {
		t.Fatalf("add target route = %q, want route-newx", got)
	}
	if section.RouteDraft.IsOpen() {
		t.Fatal("draft row should close after submit")
	}
}

func makeHarness(t *testing.T, root tui.Component) *testkit.MockAppHarness {
	t.Helper()
	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	h.Open()
	return h
}
