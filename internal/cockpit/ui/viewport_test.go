package ui

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestViewport_ResetInitializesScrollState(t *testing.T) {
	viewport := &Viewport{}

	viewport.Reset()

	if viewport.Ref == nil {
		t.Fatal("Reset should initialize Ref")
	}
	if viewport.ScrollY == nil {
		t.Fatal("Reset should initialize ScrollY")
	}
	if got := viewport.ScrollY.Get(); got != 0 {
		t.Fatalf("ScrollY after Reset = %d, want 0", got)
	}
}

func TestViewport_FollowFocusedDisabledLeavesScrollUnchanged(t *testing.T) {
	viewport := &Viewport{
		Ref:           tui.NewRef(),
		ScrollY:       tui.NewState(4),
		FollowFocused: false,
		MarginRows:    2,
	}

	viewport.FollowFocusedSelection(nil)

	if got := viewport.ScrollY.Get(); got != 4 {
		t.Fatalf("ScrollY = %d, want unchanged 4", got)
	}
}

func TestViewport_FollowFocusedOutsideViewportLeavesScrollUnchanged(t *testing.T) {
	viewport := &Viewport{
		Ref:           tui.NewRef(),
		ScrollY:       tui.NewState(4),
		FollowFocused: true,
		MarginRows:    1,
	}
	root := viewportOutsideFocusRoot(viewport)
	h, err := testkit.NewFuncHarness(root)
	if err != nil {
		t.Fatalf("NewFuncHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.FocusNext()
	viewport.FollowFocusedSelection(h.App())

	if got := viewport.ScrollY.Get(); got != 4 {
		t.Fatalf("ScrollY = %d, want unchanged 4 when focus is outside viewport", got)
	}
}

func viewportOutsideFocusRoot(viewport *Viewport) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithWidth(20),
		tui.WithHeight(8),
	)
	root.AddChild(tui.New(
		tui.WithText("outside"),
		tui.WithFocusable(true),
		tui.WithHeight(1),
	))
	body := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithWidth(20),
		tui.WithHeight(3),
		tui.WithScrollable(tui.ScrollVertical),
		tui.WithScrollOffset(0, viewport.ScrollY.Get()),
	)
	viewport.Ref.Set(body)
	for i := 0; i < 10; i++ {
		body.AddChild(tui.New(tui.WithText("body row"), tui.WithHeight(1)))
	}
	root.AddChild(body)
	return root
}
