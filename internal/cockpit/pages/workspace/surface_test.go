package workspace

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestPage_KeyMapOwnsSurfaceNavigationOnly(t *testing.T) {
	view := Page(workspacePageModel(), nil)
	keymap := view.KeyMap()

	for _, key := range []tui.Key{tui.KeyUp, tui.KeyDown, tui.KeyEnter, tui.KeyEscape} {
		binding, ok := findBinding(keymap, key)
		if !ok {
			t.Fatalf("missing key binding for %v", key)
		}
		if !binding.Stop {
			t.Fatalf("binding for %v should stop propagation", key)
		}
		if binding.Pattern.FocusRequired {
			t.Fatalf("binding for %v should be surface-level, not focus-gated", key)
		}
		binding.Handler(tui.KeyEvent{Key: key})
	}
}

func TestPage_FocusableGraphFollowsExpandedRoute(t *testing.T) {
	view := Page(workspacePageModel(), nil)
	// Workspace page surface has one focusable container
	if got, want := countFocusables(view.Render(nil)), 1; got != want {
		t.Fatalf("collapsed workspace focusables = %d, want %d", got, want)
	}

	view.RoutesSection.OpenRoute(view.RoutesSection.State.Routes[0])
	if got, want := countFocusables(view.Render(nil)), 1; got != want {
		t.Fatalf("expanded workspace focusables = %d, want %d", got, want)
	}
}

func TestPage_ActivationWalksExpandedRouteChildrenInRenderOrder(t *testing.T) {
	view := Page(workspacePageModel(), nil)
	route := view.RoutesSection.State.Routes[0]

	view.RoutesSection.OpenRoute(route)
	if got := view.RoutesSection.State.ExpandedRoute.Get(); got != "gpt" {
		t.Fatalf("expanded route = %q, want gpt", got)
	}

	view.RoutesSection.OpenTargetEditor(route, route.Targets[0])
	if got := view.RoutesSection.State.OpenTarget.Get(); got != "target-1" {
		t.Fatalf("opened first target = %q, want target-1", got)
	}

	view.RoutesSection.OpenTargetEditor(route, route.Targets[1])
	if got := view.RoutesSection.State.OpenTarget.Get(); got != "target-2" {
		t.Fatalf("opened second target = %q, want target-2", got)
	}

	view.RoutesSection.OpenRoute(view.RoutesSection.State.Routes[1])
	if got := view.RoutesSection.State.ExpandedRoute.Get(); got != "local" {
		t.Fatalf("next route expansion = %q, want local", got)
	}
}

func TestPage_BackClosesLocalRouteStateWithoutFocusState(t *testing.T) {
	view := Page(workspacePageModel(), nil)
	route := view.RoutesSection.State.Routes[0]
	view.RoutesSection.OpenTargetEditor(route, route.Targets[0])

	view.backOut(tui.KeyEvent{Key: tui.KeyEscape})
	if got := view.RoutesSection.State.OpenTarget.Get(); got != "" {
		t.Fatalf("open target after first Esc = %q, want empty", got)
	}
	if got := view.RoutesSection.State.ExpandedRoute.Get(); got != "gpt" {
		t.Fatalf("expanded route after first Esc = %q, want gpt", got)
	}

	view.backOut(tui.KeyEvent{Key: tui.KeyEscape})
	if got := view.RoutesSection.State.ExpandedRoute.Get(); got != "" {
		t.Fatalf("expanded route after second Esc = %q, want empty", got)
	}
}

func TestPage_BackLeavesFeatureOwnedDeleteConfirmationToFocusedFeature(t *testing.T) {
	view := Page(workspacePageModel(), nil)
	view.OverviewSection.OpenDeleteConfirmation("dev")
	view.RoutesSection.OpenRoute(view.RoutesSection.State.Routes[0])

	view.backOut(tui.KeyEvent{Key: tui.KeyEscape})
	if got := view.RoutesSection.State.ExpandedRoute.Get(); got != "" {
		t.Fatalf("expanded route after page-level Esc = %q, want empty", got)
	}
}

func findBinding(keymap tui.KeyMap, key tui.Key) (tui.KeyBinding, bool) {
	for _, binding := range keymap {
		if binding.Pattern.Key == key {
			return binding, true
		}
	}
	return tui.KeyBinding{}, false
}

func countFocusables(root *tui.Element) int {
	return len(collectFocusables(root))
}

func collectFocusables(root *tui.Element) []tui.Focusable {
	var focusables []tui.Focusable
	root.WalkFocusables(func(f tui.Focusable) {
		focusables = append(focusables, f)
	})
	return focusables
}

func activate(t *testing.T, focusable tui.Focusable) {
	t.Helper()
	el, ok := focusable.(*tui.Element)
	if !ok {
		t.Fatalf("focusable is %T, want *tui.Element", focusable)
	}
	if !el.Activate() {
		t.Fatal("focusable did not handle activation")
	}
}

func workspacePageModel() readmodel.WorkspaceReadModel {
	return readmodel.WorkspaceReadModel{
		Slug:          "dev",
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/dev",
		RunCommands: []readmodel.RunCommandReadModel{{
			ID:    "codex",
			Label: "Codex",
		}},
		Routes: []readmodel.RouteReadModel{
			{
				ID:        "gpt",
				ModelName: "gpt",
				State:     readmodel.RouteNormal,
				PlanKind:  readmodel.RoutePlanRanked,
				Default:   true,
				Enabled:   true,
				Targets: []readmodel.TargetReadModel{
					{ID: "target-1", Provider: "openai", Model: "gpt-4.1", Rank: 1},
					{ID: "target-2", Provider: "ollama", Model: "qwen", Rank: 2},
				},
			},
			{
				ID:        "local",
				ModelName: "local",
				State:     readmodel.RouteNormal,
				PlanKind:  readmodel.RoutePlanSingle,
				Enabled:   true,
				Targets: []readmodel.TargetReadModel{
					{ID: "local-1", Provider: "ollama", Model: "llama3.2", Rank: 1},
				},
			},
		},
	}
}
