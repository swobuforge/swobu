package routes

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestSection_FocusableRowsFollowExpansion(t *testing.T) {
	model := routeSectionModel()

	collapsed := Section(model).Render(nil)
	if got, want := countFocusables(collapsed), 2; got != want {
		t.Fatalf("collapsed focusables = %d, want %d", got, want)
	}

	model.View.ExpandedRouteID = "gpt"
	expanded := Section(model).Render(nil)
	if got, want := countFocusables(expanded), 5; got != want {
		t.Fatalf("expanded focusables = %d, want %d", got, want)
	}
}

func TestRouteParentRow_ActivationTogglesExpansion(t *testing.T) {
	section := Section(routeSectionModel())
	route := section.Model.Routes[0]
	focusables := collectFocusables(section.Render(nil))

	activate(t, focusables[0])
	if got := section.ExpandedRoute.Get(); got != route.ID {
		t.Fatalf("expanded route after first Enter = %q, want %q", got, route.ID)
	}

	focusables = collectFocusables(section.Render(nil))
	activate(t, focusables[0])
	if got := section.ExpandedRoute.Get(); got != "" {
		t.Fatalf("expanded route after second Enter = %q, want empty", got)
	}
}

func TestTargetChildRow_ActivationOpensTargetWithoutCollapsingParent(t *testing.T) {
	section := Section(routeSectionModel())
	route := section.Model.Routes[0]
	target := route.Targets[0]
	section.ExpandedRoute.Set(route.ID)
	focusables := collectFocusables(section.Render(nil))

	activate(t, focusables[1])
	if got := section.OpenTarget.Get(); got != target.ID {
		t.Fatalf("opened target = %q, want %q", got, target.ID)
	}
	if got := section.ExpandedRoute.Get(); got != route.ID {
		t.Fatalf("expanded route = %q, want still %q", got, route.ID)
	}
}

func TestAddTargetRow_ActivationRecordsLocalIntent(t *testing.T) {
	section := Section(routeSectionModel())
	route := section.Model.Routes[0]
	section.ExpandedRoute.Set(route.ID)
	focusables := collectFocusables(section.Render(nil))

	activate(t, focusables[3])
	if got := section.AddTargetRoute.Get(); got != route.ID {
		t.Fatalf("add target route = %q, want %q", got, route.ID)
	}
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

func routeSectionModel() readmodel.WorkspaceReadModel {
	return readmodel.WorkspaceReadModel{
		View: readmodel.WorkspaceViewState{RoutesExpanded: true},
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
