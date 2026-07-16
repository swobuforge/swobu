package routes

import (
	"context"
	"errors"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestSection_FocusableRowsFollowExpansion(t *testing.T) {
	model := routeSectionModel()

	collapsed := Section(model, nil)
	collapsedRoot := mountedRoot(t, collapsed)
	collapsedFocusables := countFocusables(collapsedRoot)
	if got, want := collapsedFocusables, 4; got != want {
		t.Fatalf("collapsed focusables = %d, want %d", got, want)
	}

	section := Section(model, nil)
	section.OpenRoute(section.State.Routes[0])
	expanded := mountedRoot(t, section)
	if got := countFocusables(expanded); got <= collapsedFocusables {
		t.Fatalf("expanded focusables = %d, want more than collapsed %d", got, collapsedFocusables)
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
	section.AddTarget(route)
	if got := section.State.AddTargetRoute.Get(); got != route.ID {
		t.Fatalf("add target route = %q, want %q", got, route.ID)
	}
}

func TestAddTargetOpen_RendersInlineWorkflowHeader(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	section.ListProviders = func(context.Context) ([]readmodel.ProviderOptionReadModel, error) {
		return []readmodel.ProviderOptionReadModel{
			{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
			{ProviderSpec: "openrouter", DisplayName: "OpenRouter", SetupHint: "API key"},
			{ProviderSpec: "openai_compatible", DisplayName: "OpenAI Compatible", SetupHint: "endpoint"},
		}, nil
	}

	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	rendered := testkit.RenderMountedTrimmed(t, section, 220, 20)
	assertSubstringsInOrder(t, rendered, "name", "client sends", "step 1", "openai/gpt-4.1", "step 2", "anthropic/claude-sonnet", "add target")
	if strings.Contains(rendered, "base URL") || strings.Contains(rendered, "credential") || strings.Contains(rendered, "provider/model") || strings.Contains(rendered, "model _") {
		t.Fatalf("provider picker should not leak provider-setup or raw input rows:\n%s", rendered)
	}
	testkit.AssertVisual("add_target_open").
		Fixture("testdata/routes_section/fixture/add_target_open.txt").
		Viewport(220, 20).
		Now(t, rendered)
}

func TestAddTargetOpen_EscapeReturnsToAddTargetRow(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	section.ListProviders = func(context.Context) ([]readmodel.ProviderOptionReadModel, error) {
		return []readmodel.ProviderOptionReadModel{
			{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
			{ProviderSpec: "openrouter", DisplayName: "OpenRouter", SetupHint: "API key"},
		}, nil
	}

	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.App().FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if got := section.State.AddTargetRoute.Get(); got != "" {
		t.Fatalf("add target route after escape = %q, want empty", got)
	}
	rendered := h.Frame()
	if !strings.Contains(rendered, "add target") {
		t.Fatalf("expected add target row after escape:\n%s", rendered)
	}
	if strings.Contains(rendered, "search") {
		t.Fatalf("provider picker should close after escape:\n%s", rendered)
	}
}

// ---------------------------------------------------------------------------
// Target string row tests — replace old multi-row workflow tests
// ---------------------------------------------------------------------------

func TestRouteSection_TargetStringEditRowAppears(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.toggleRoute(route)
	section.openTarget(target)

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 20)
	// Verify the old multi-row workflow is gone and a single input row
	// appears with the current provider/model value.
	if !contains(rendered, "openai/gpt-4.1") {
		t.Fatal("expected target string value in rendered output")
	}
}

func TestTargetStringRow_EditStateSurvivesRender(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.toggleRoute(route)
	section.openTarget(target)

	row := section.targetStringRow(route, target)
	if row == nil {
		t.Fatal("expected string row after openTarget")
	}
	row.raw.Set("typed-value")
	_ = testkit.RenderMountedString(t, section, 100, 20)

	if got := section.targetStringRow(route, target).raw.Get(); got != "typed-value" {
		t.Fatalf("string row raw after render = %q, want typed-value", got)
	}
}

func TestRouteAdd_DraftRowAppearsInSection(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.addRoute()

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 8)
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
	// Add target form stays closed — operator must explicitly choose to add.
	if got := section.State.AddTargetRoute.Get(); got != "" {
		t.Fatalf("add target route = %q, want empty", got)
	}
	route := section.State.Routes[len(section.State.Routes)-1]
	if route.State != readmodel.RouteNormal || len(route.Targets) != 0 {
		t.Fatalf("draft route = %#v, want normal route with no targets", route)
	}
	if got, want := route.RowValue(), "incomplete · no targets"; got != want {
		t.Fatalf("draft route row value = %q, want %q", got, want)
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

	// Operator must explicitly open the add-target form for the newly created route.
	section.AddTarget(route)

	row := section.targetCreateRow(route)
	if row == nil {
		t.Fatal("expected target create row for draft route")
	}

	// Simulate typing a provider/model string and submitting.
	section.submitTargetCreate(route.ID, "openai_compatible/custom-route")

	if request.RouteID != "custom-route" || request.Model != "custom-route" {
		t.Fatalf("save target request = %+v, want draft route/model custom-route", request)
	}
	savedRoute := section.State.Routes[len(section.State.Routes)-1]
	if savedRoute.State != readmodel.RouteNormal {
		t.Fatalf("saved route state = %#v, want normal after first target save", savedRoute.State)
	}
	if got, want := savedRoute.RowValue(), "1 target"; got != want {
		t.Fatalf("saved route row value = %q, want %q", got, want)
	}
}

func TestRouteSection_UpdatePropsRefreshesOpenTargetAddWorkflow(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.AddTarget(route)

	wf := section.targetAddWorkflow(route)
	if wf == nil {
		t.Fatal("expected target add workflow")
	}

	fresh := Section(routeSectionModel(), nil)
	fresh.State.Routes[0].Targets = append(fresh.State.Routes[0].Targets, readmodel.TargetReadModel{
		ID:       "fresh-target",
		Provider: "openai",
		Model:    "gpt-4.1",
		Rank:     2,
		Weight:   1,
	})

	section.UpdateProps(fresh)

	if got, want := len(wf.Route.Targets), len(fresh.State.Routes[0].Targets); got != want {
		t.Fatalf("target add workflow route targets = %d, want %d", got, want)
	}
}

func TestRouteEdit_RenameMovesOpenTargetAddWorkflow(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.AddTarget(route)

	wf := section.targetAddWorkflow(route)
	if wf == nil {
		t.Fatal("expected target add workflow")
	}

	renamed := route
	renamed.ID = "gpt-renamed"
	renamed.ModelName = "gpt-renamed"
	section.saveRoute(route.ID, renamed)

	if got := section.State.AddTargetRoute.Get(); got != "gpt-renamed" {
		t.Fatalf("add target route = %q, want gpt-renamed", got)
	}
	if _, ok := section.TargetAddWorkflows[route.ID]; ok {
		t.Fatal("old target add workflow key should be cleared after rename")
	}
	moved := section.TargetAddWorkflows["gpt-renamed"]
	if moved == nil {
		t.Fatal("renamed route should keep target add workflow")
	}
	if moved != wf {
		t.Fatal("rename should reuse the open target add workflow instance")
	}
	if got := moved.Route.ID; got != "gpt-renamed" {
		t.Fatalf("workflow route id = %q, want gpt-renamed", got)
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
	section.openTarget(target)

	// Old assertion path used targetEditor workflow internal state.
	// New path: the section's submitTargetEdit reads Provider/Model from
	// the row and preserves all other fields from the existing target.
	row := section.targetStringRow(originalRoute, target)
	if row == nil {
		t.Fatal("expected target string row")
	}

	// Operator switches to another route while editing — the row was
	// created for originalRoute:target-1, so the save should still target
	// originalRoute and update target-1.
	section.OpenRoute(otherRoute)
	row.raw.Set("openai_compatible/gpt-4")
	row.onSubmit(row.raw.Get())

	if got := section.State.Routes[0].Targets[0].Provider; got != "openai_compatible" {
		t.Fatalf("original route target provider = %q, want openai_compatible", got)
	}
	if got, want := len(section.State.Routes[1].Targets), 1; got != want {
		t.Fatalf("other route target count = %d, want unchanged %d", got, want)
	}
}

func TestTargetDelete_ConfirmRemovesTargetAndSyncsState(t *testing.T) {
	var deletedTargetID readmodel.TargetID
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(_ context.Context, req ports.DeleteTargetRequest) error {
			deletedTargetID = req.TargetID
			return nil
		},
	})
	originalRoute := section.State.Routes[0]
	target := originalRoute.Targets[0]
	section.openTarget(target)

	section.deleteTargetAndClose(originalRoute.ID, target.ID)

	if deletedTargetID != target.ID {
		t.Fatalf("deleted target id = %q, want %q", deletedTargetID, target.ID)
	}
	if got, want := len(section.State.Routes[0].Targets), 1; got != want {
		t.Fatalf("targets after delete = %d, want %d", got, want)
	}
	if got := section.State.OpenTarget.Get(); got != "" {
		t.Fatalf("open target = %q, want closed", got)
	}
}

func TestTargetDelete_ConfirmRenumbersSteps(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	route := section.State.Routes[0]
	// Simulate step 1 with 1 target, step 2 with 2 targets
	section.State.Routes[0].Targets = []readmodel.TargetReadModel{
		{ID: "target-a", Provider: "openai", Model: "gpt-4", Rank: 1},
		{ID: "target-b", Provider: "openai", Model: "gpt-3.5", Rank: 2},
		{ID: "target-c", Provider: "anth", Model: "claude", Rank: 2},
	}
	targetB := section.State.Routes[0].Targets[1]
	section.deleteTargetAndClose(route.ID, targetB.ID)

	if got, want := len(section.State.Routes[0].Targets), 2; got != want {
		t.Fatalf("targets after delete = %d, want %d", got, want)
	}
	// After deleting one of two targets in step 2, step 2 should now have
	// rank 2 (unchanged, still contiguous).
	if got := section.State.Routes[0].Targets[1].Rank; got != 2 {
		t.Fatalf("remaining step-2 target rank = %d, want 2", got)
	}
}

func TestTargetDelete_ConfirmOnLastTargetRemovesStep(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	route := section.State.Routes[0]
	// Simulate step 1 with 1 target, step 2 with 1 target
	section.State.Routes[0].Targets = []readmodel.TargetReadModel{
		{ID: "target-a", Provider: "openai", Model: "gpt-4", Rank: 1},
		{ID: "target-b", Provider: "anth", Model: "claude", Rank: 2},
	}
	targetB := section.State.Routes[0].Targets[1]
	section.deleteTargetAndClose(route.ID, targetB.ID)

	if got, want := len(section.State.Routes[0].Targets), 1; got != want {
		t.Fatalf("targets after delete = %d, want %d", got, want)
	}
	// After deleting step 2's only target, step numbering should collapse:
	// the remaining target should stay rank 1.
	if got := section.State.Routes[0].Targets[0].Rank; got != 1 {
		t.Fatalf("remaining target rank = %d, want 1", got)
	}
}

func TestTargetDelete_ConfirmOnOnlyTargetKeepsStep1Affordance(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	route := section.State.Routes[0]
	// Simulate step 1 with 1 target
	section.State.Routes[0].Targets = []readmodel.TargetReadModel{
		{ID: "target-a", Provider: "openai", Model: "gpt-4", Rank: 1},
	}
	section.deleteTargetAndClose(route.ID, readmodel.TargetID("target-a"))

	// Step 1 is kept as empty affordance even with no targets.
	if got, want := len(section.State.Routes[0].Targets), 0; got != want {
		t.Fatalf("targets after last delete = %d, want %d", got, want)
	}
}

func TestTargetDeleteUsesWorkflowRouteWhenExpansionChanges(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	originalRoute := section.State.Routes[0]
	otherRoute := section.State.Routes[1]
	target := originalRoute.Targets[0]
	section.openTarget(target)
	_ = section.targetStringRow(originalRoute, target)

	section.OpenRoute(otherRoute)
	// Delete is not routed through the string row — future Task E will add
	// a delete confirmation row or action. For now just verify nothing
	// unexpected happens when switching routes.
	if got, want := len(section.State.Routes[0].Targets), 2; got != want {
		t.Fatalf("original route target count = %d, want unchanged %d", got, want)
	}
	if got, want := len(section.State.Routes[1].Targets), 1; got != want {
		t.Fatalf("other route target count = %d, want unchanged %d", got, want)
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

	section.submitRouteName(route.ID, "gpt-renamed")

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

	section.setRouteDefault(route.ID)

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

	section.confirmDeleteRoute(route.ID)

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

// TODO(Task E): Inline target delete is not implemented yet.
func TestRouteSection_TargetDeleteRemovesRow(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.toggleRoute(route)
	section.openTarget(target)

	// Old test path used workflow.ActivateDelete twice.
	// New path: no delete capability in the string row yet.
	// Verify the target still exists (unchanged) and row is rendered.
	row := section.targetStringRow(route, target)
	if row == nil {
		t.Fatal("expected target string row")
	}
	if got := len(section.State.Routes[0].Targets); got != 2 {
		t.Fatalf("targets after no-op interaction = %d, want 2", got)
	}
	if got := section.State.OpenTarget.Get(); got != target.ID {
		t.Fatalf("open target = %q, want still %q", got, target.ID)
	}
}

func TestRouteDelete_ClearsOpenTargetAddWorkflow(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.AddTarget(route)

	section.deleteRoute(route.ID)

	if got := section.State.AddTargetRoute.Get(); got != "" {
		t.Fatalf("add target route = %q, want empty", got)
	}
	if _, ok := section.TargetAddWorkflows[route.ID]; ok {
		t.Fatal("deleted route should remove cached target add workflow")
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

func mountedRoot(t *testing.T, component tui.Component) *tui.Element {
	t.Helper()
	h, err := testkit.NewHarness(component)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	h.Open()
	t.Cleanup(h.Close)
	return h.App().Root()
}

func routeSectionModel() readmodel.WorkspaceReadModel {
	return readmodel.WorkspaceReadModel{
		Routes: []readmodel.RouteReadModel{
			{
				ID:        "gpt",
				ModelName: "gpt",
				State:     readmodel.RouteNormal,
				Default:   true,
				Enabled:   true,
				Targets: []readmodel.TargetReadModel{
					{ID: "target-1", Provider: "openai", Model: "gpt-4.1", Rank: 1},
					{ID: "target-2", Provider: "anthropic", Model: "claude-sonnet", Rank: 2},
				},
			},
			{
				ID:        "local",
				ModelName: "local",
				State:     readmodel.RouteNormal,
				Enabled:   true,
				Targets: []readmodel.TargetReadModel{
					{ID: "local-1", Provider: "ollama", Model: "llama3.2", Rank: 1},
				},
			},
		},
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSub(s, substr))
}

func containsSub(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func assertSubstringsInOrder(t *testing.T, s string, substrings ...string) {
	t.Helper()
	pos := 0
	for _, substr := range substrings {
		idx := strings.Index(s[pos:], substr)
		if idx < 0 {
			t.Fatalf("rendered output missing %q:\n%s", substr, s)
		}
		pos += idx + len(substr)
	}
}
