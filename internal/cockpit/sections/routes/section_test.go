package routes

import (
	"context"
	"errors"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestSection_FocusableRowsFollowExpansion(t *testing.T) {
	model := routeSectionModel()

	// With no app, all rows are inert — no focusables.
	collapsed := Section(model, nil).Render(nil)
	if got, want := countFocusables(collapsed), 0; got != want {
		t.Fatalf("collapsed focusables = %d, want %d", got, want)
	}

	section := Section(model, nil)
	section.State.ExpandedRoute.Set("gpt")
	expanded := section.Render(nil)
	if got, want := countFocusables(expanded), 0; got != want {
		t.Fatalf("expanded focusables = %d, want %d", got, want)
	}
}

func TestRouteParentRow_ActivationTogglesExpansion(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.toggleRoute(route)
	if got := section.State.ExpandedRoute.Get(); got != route.ID {
		t.Fatalf("expanded route after toggle = %q, want %q", got, route.ID)
	}
	section.toggleRoute(route)
	if got := section.State.ExpandedRoute.Get(); got != "" {
		t.Fatalf("expanded route after second toggle = %q, want empty", got)
	}
}

func TestTargetChildRow_ActivationOpensTargetWithoutCollapsingParent(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.openTarget(target)
	if got := section.State.OpenTarget.Get(); got != target.ID {
		t.Fatalf("opened target = %q, want %q", got, target.ID)
	}
	if got := section.State.ExpandedRoute.Get(); got != route.ID {
		t.Fatalf("expanded route = %q, want still %q", got, route.ID)
	}
}

func TestAddTargetRow_ActivationRecordsLocalIntent(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.addTarget(route)
	if got := section.State.AddTargetRoute.Get(); got != route.ID {
		t.Fatalf("add target route = %q, want %q", got, route.ID)
	}
}

func TestRouteSection_TargetEditFormAppearsInExpandedRoute(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.toggleRoute(route)
	section.openTarget(target)

	rendered := testkit.RenderTrimmed(section.Render(nil), 100, 20)
	testkit.AssertVisual("target_edit_open").
		Fixture("testdata/routes_section/fixture/target_edit_open.txt").
		Viewport(100, 20).
		Now(t, rendered)
}

func TestRouteSection_WorkflowStateSurvivesRender(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.toggleRoute(route)

	routeWorkflow := section.routeEditor(route)
	routeWorkflow.ActivateName()
	routeWorkflow.ModelName.Set("typed-route-name")
	section.Render(nil)
	if got := section.routeEditor(route).ModelName.Get(); got != "typed-route-name" {
		t.Fatalf("route workflow model after render = %q, want typed-route-name", got)
	}

	section.openTarget(target)
	targetWorkflow := section.targetEditor(route, target)
	targetWorkflow.Provider.Set("typed-provider")
	section.Render(nil)
	if got := section.targetEditor(route, target).Provider.Get(); got != "typed-provider" {
		t.Fatalf("target workflow provider after render = %q, want typed-provider", got)
	}
}

func TestRouteAdd_DraftRowAppearsInSection(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.addRoute()

	rendered := testkit.RenderTrimmed(section.Render(nil), 100, 8)
	testkit.AssertVisual("draft_row").
		Fixture("testdata/routes_section/fixture/draft_row.txt").
		Viewport(100, 8).
		Now(t, rendered)
}

func TestRouteAdd_CreateOpensTargetAddForThisRoute(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.addRoute()
	section.RouteDraft.ModelName.Set("custom-route")

	section.createDraftRoute()

	if got := section.State.ExpandedRoute.Get(); got != "custom-route" {
		t.Fatalf("expanded route = %q, want custom-route", got)
	}
	if got := section.State.AddTargetRoute.Get(); got != "custom-route" {
		t.Fatalf("add target route = %q, want custom-route", got)
	}
	route := section.State.Routes[len(section.State.Routes)-1]
	if route.State != readmodel.RouteIncomplete || len(route.Targets) != 0 {
		t.Fatalf("draft route = %#v, want incomplete route with no targets", route)
	}
}

func TestRouteAdd_FirstTargetSaveUsesDraftRoute(t *testing.T) {
	var request ports.SaveTargetRequest
	section := Section(routeSectionModel(), fakeRouteCommands{
		saveTarget: func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
			request = req
			return readmodel.TargetReadModel{ID: "target-new", Provider: req.Provider, Model: req.Model, Rank: req.Rank, Weight: req.Weight}, nil
		},
	})
	section.addRoute()
	section.RouteDraft.ModelName.Set("custom-route")
	section.createDraftRoute()
	route := section.State.Routes[len(section.State.Routes)-1]

	workflow := section.targetCreator(route)
	if workflow == nil {
		t.Fatal("expected create workflow for draft route")
	}
	workflow.Provider.Set("openai_compatible")
	workflow.Submit(context.Background())

	if request.RouteID != "custom-route" || request.Model != "custom-route" {
		t.Fatalf("save target request = %+v, want draft route/model custom-route", request)
	}
}

func TestTargetSaveUsesWorkflowRouteWhenExpansionChanges(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		saveTarget: func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
			return readmodel.TargetReadModel{
				ID:       req.TargetID,
				Provider: req.Provider,
				Model:    req.Model,
				Rank:     req.Rank,
				Weight:   req.Weight,
			}, nil
		},
	})
	originalRoute := section.State.Routes[0]
	otherRoute := section.State.Routes[1]
	target := originalRoute.Targets[0]
	section.OpenTargetEditor(originalRoute, target)
	workflow := section.targetEditor(originalRoute, target)
	if workflow == nil {
		t.Fatal("expected edit workflow")
	}

	section.OpenRoute(otherRoute)
	workflow.Provider.Set("provider-after-route-switch")
	workflow.Submit(context.Background())

	if got := section.State.Routes[0].Targets[0].Provider; got != "provider-after-route-switch" {
		t.Fatalf("original route target provider = %q, want provider-after-route-switch", got)
	}
	if got := len(section.State.Routes[1].Targets); got != 1 {
		t.Fatalf("other route target count = %d, want unchanged 1", got)
	}
}

func TestTargetDeleteUsesWorkflowRouteWhenExpansionChanges(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	originalRoute := section.State.Routes[0]
	otherRoute := section.State.Routes[1]
	target := originalRoute.Targets[0]
	section.OpenTargetEditor(originalRoute, target)
	workflow := section.targetEditor(originalRoute, target)
	if workflow == nil {
		t.Fatal("expected edit workflow")
	}

	section.OpenRoute(otherRoute)
	workflow.ActivateDelete()
	workflow.ActivateDelete()

	if got := len(section.State.Routes[0].Targets); got != 1 {
		t.Fatalf("original route target count = %d, want deleted to 1", got)
	}
	if section.State.Routes[0].Targets[0].ID == target.ID {
		t.Fatal("deleted target still present on original route")
	}
	if got := len(section.State.Routes[1].Targets); got != 1 {
		t.Fatalf("other route target count = %d, want unchanged 1", got)
	}
}

func TestRouteEdit_RenameExistingRoute(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		saveRoute: func(_ context.Context, req ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
			route := routeSectionModel().Routes[0]
			route.ID = readmodel.RouteID(req.ModelName)
			route.ModelName = req.ModelName
			return route, nil
		},
	})
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	workflow := section.ensureRouteEditor(route)
	workflow.ModelName.Set("gpt-renamed")

	workflow.Submit(context.Background())

	if section.State.Routes[0].ID != "gpt-renamed" || section.State.Routes[0].ModelName != "gpt-renamed" {
		t.Fatalf("renamed route = %#v, want gpt-renamed", section.State.Routes[0])
	}
	if section.State.ExpandedRoute.Get() != "gpt-renamed" {
		t.Fatalf("expanded route = %q, want gpt-renamed", section.State.ExpandedRoute.Get())
	}
}

func TestRouteEdit_SetDefaultUpdatesOnlyOneRoute(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		saveRoute: func(_ context.Context, req ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
			route := routeSectionModel().Routes[1]
			route.Default = req.Default
			return route, nil
		},
	})
	route := section.State.Routes[1]

	section.ensureRouteEditor(route).SetDefault(context.Background())

	if section.State.Routes[0].Default {
		t.Fatal("previous default route should no longer be default")
	}
	if !section.State.Routes[1].Default {
		t.Fatal("selected route should be default")
	}
}

func TestRouteDelete_RemovesRouteAndRefreshes(t *testing.T) {
	var request ports.DeleteRouteRequest
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteRoute: func(_ context.Context, req ports.DeleteRouteRequest) error {
			request = req
			return nil
		},
	})
	route := section.State.Routes[1]
	section.State.ExpandedRoute.Set(route.ID)
	workflow := section.ensureRouteEditor(route)

	workflow.ActivateDelete()
	workflow.ActivateDelete()

	if request.RouteID != route.ID {
		t.Fatalf("delete route request = %+v, want route %q", request, route.ID)
	}
	if got := len(section.State.Routes); got != 1 {
		t.Fatalf("routes after delete = %d, want 1", got)
	}
	if section.State.ExpandedRoute.Get() != "" {
		t.Fatalf("expanded route = %q, want empty", section.State.ExpandedRoute.Get())
	}
}

func TestRouteSection_TargetDeleteRemovesRow(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.toggleRoute(route)
	section.openTarget(target)

	workflow := section.targetEditor(route, target)
	if workflow == nil {
		t.Fatal("expected edit workflow")
	}
	workflow.ActivateDelete()
	workflow.ActivateDelete()

	if got := len(section.State.Routes[0].Targets); got != 1 {
		t.Fatalf("remaining targets = %d, want 1", got)
	}
	if got := section.State.OpenTarget.Get(); got != "" {
		t.Fatalf("open target = %q, want empty", got)
	}
}

type fakeRouteCommands struct {
	saveRoute    func(context.Context, ports.SaveRouteRequest) (readmodel.RouteReadModel, error)
	saveTarget   func(context.Context, ports.SaveTargetRequest) (readmodel.TargetReadModel, error)
	deleteRoute  func(context.Context, ports.DeleteRouteRequest) error
	deleteTarget func(context.Context, ports.DeleteTargetRequest) error
}

func (f fakeRouteCommands) SaveRoute(ctx context.Context, request ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
	if f.saveRoute != nil {
		return f.saveRoute(ctx, request)
	}
	return readmodel.RouteReadModel{
		ID:        readmodel.RouteID(request.ModelName),
		ModelName: request.ModelName,
		Enabled:   request.Enabled,
		Default:   request.Default,
	}, nil
}

func (f fakeRouteCommands) DeleteRoute(ctx context.Context, request ports.DeleteRouteRequest) error {
	if f.deleteRoute != nil {
		return f.deleteRoute(ctx, request)
	}
	return nil
}

func (f fakeRouteCommands) SaveTarget(ctx context.Context, request ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
	if f.saveTarget != nil {
		return f.saveTarget(ctx, request)
	}
	return readmodel.TargetReadModel{
		ID:       request.TargetID,
		Provider: request.Provider,
		Model:    request.Model,
		Rank:     request.Rank,
		Weight:   request.Weight,
	}, nil
}

func (f fakeRouteCommands) DeleteTarget(ctx context.Context, request ports.DeleteTargetRequest) error {
	if f.deleteTarget != nil {
		return f.deleteTarget(ctx, request)
	}
	return errors.New("delete target not wired")
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
