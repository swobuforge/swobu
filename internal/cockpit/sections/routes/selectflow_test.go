package routes

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestRouteSection_FocusTraversal(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.State.ExpandedRoute.Set(section.State.Routes[0].ID)
	h := makeHarness(t, section)
	defer h.Close()

	// All selectable rows should be in focus tree.
	// Expanded route has: route row + 3 detail rows + 2 target rows + add target row = 7.
	// With the detail rows now inline: route row + 3 detail + 2 targets + add target = 10.
	root := h.App().Root()
	if root == nil {
		t.Fatal("app.Root() returned nil")
	}
	focusables := collectFocusables(root)
	if got, want := len(focusables), 10; got != want {
		t.Fatalf("expanded route focusables = %d, want %d", got, want)
	}
}

func TestRouteSection_FocusMarkersRendered(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.State.ExpandedRoute.Set(section.State.Routes[0].ID)
	h := makeHarness(t, section)
	defer h.Close()

	testkit.AssertFocusVisible(t, h, h.FocusNext, "> ")
}

func TestRouteSection_FocusMoveUpdatesMarker(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.State.ExpandedRoute.Set(section.State.Routes[0].ID)
	h := makeHarness(t, section)
	defer h.Close()

	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

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
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	frameBefore := h.Frame()
	testkit.AssertFocusedFrame(t, frameBefore, ">    gpt")

	testkit.AssertFocusVisible(t, h, func() {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	}, "> ")

	if section.State.ExpandedRoute.Get() != "" {
		t.Fatalf("expected route toggled closed after activation, got %q", section.State.ExpandedRoute.Get())
	}
}

func TestRouteSection_SpaceActivatesRow(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.State.ExpandedRoute.Set(section.State.Routes[0].ID)
	h := makeHarness(t, section)
	defer h.Close()

	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
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
	// Add target form stays closed — operator must explicitly choose to add.
	if got := section.State.AddTargetRoute.Get(); got != "" {
		t.Fatalf("add target route = %q, want closed", got)
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
