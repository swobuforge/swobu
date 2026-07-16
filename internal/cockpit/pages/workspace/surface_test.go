package workspace

import (
	"context"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestPage_KeyMapOwnsSurfaceNavigationAndFallbackActivation(t *testing.T) {
	view := Page(workspacePageModel(), nil, nil, nil, context.Background(), nil)
	keymap := view.KeyMap()

	for _, key := range []tui.Key{tui.KeyUp, tui.KeyDown, tui.KeyEscape, tui.KeyEnter} {
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
	if binding, ok := findRuneBinding(keymap, ' '); ok {
		t.Fatalf("workspace page must not own rune activation directly, found binding %+v", binding)
	}
	for _, binding := range keymap {
		if binding.Pattern.AnyRune {
			t.Fatalf("workspace page must not own arbitrary rune input via AnyRune, found binding %+v", binding)
		}
	}
}

func TestPage_DraftWorkspaceHidesRoutesAndActivity(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		view := Page(readmodel.WorkspaceReadModel{State: readmodel.WorkspaceDraft}, nil, nil, nil, context.Background(), nil)
		rendered := testkit.RenderMountedTrimmed(t, view, width, 12)
		if strings.Contains(rendered, "workspace ▾") {
			t.Fatalf("draft workspace should not render a disclosure header at width %d:\n%s", width, rendered)
		}
		if !strings.Contains(rendered, "new workspace") {
			t.Fatalf("draft workspace should render the create header at width %d:\n%s", width, rendered)
		}
		if strings.Contains(rendered, "model routes") {
			t.Fatalf("draft workspace should not render routes at width %d:\n%s", width, rendered)
		}
		if strings.Contains(rendered, "activity") {
			t.Fatalf("draft workspace should not render activity at width %d:\n%s", width, rendered)
		}
		if !strings.Contains(rendered, "slug") || !strings.Contains(rendered, "required") || !strings.Contains(rendered, "after create") {
			t.Fatalf("draft workspace should still render the create flow at width %d:\n%s", width, rendered)
		}
	}
}

func TestPage_FocusableGraphFollowsExpandedRoute(t *testing.T) {
	view := Page(workspacePageModel(), nil, nil, nil, context.Background(), nil)
	collapsedFocusables := countFocusables(mountedRoot(t, view))
	if collapsedFocusables == 0 {
		t.Fatal("collapsed workspace should expose mounted focusables")
	}

	view.RoutesSection.OpenRoute(view.RoutesSection.State.Routes[0])
	if got := countFocusables(mountedRoot(t, view)); got <= collapsedFocusables {
		t.Fatalf("expanded workspace focusables = %d, want more than collapsed %d", got, collapsedFocusables)
	}
}

func TestPage_ActivationWalksExpandedRouteChildrenInRenderOrder(t *testing.T) {
	view := Page(workspacePageModel(), nil, nil, nil, context.Background(), nil)
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
	view := Page(workspacePageModel(), nil, nil, nil, context.Background(), nil)
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
	view := Page(workspacePageModel(), nil, nil, nil, context.Background(), nil)
	view.OverviewSection.OpenDeleteConfirmation("dev")

	view.backOut(tui.KeyEvent{Key: tui.KeyEscape})
	if got := view.OverviewSection.PendingDeleteWorkspaceID.Get(); got != "" {
		t.Fatalf("delete confirmation should close, got pending delete workspace id = %q", got)
	}
}

func TestPage_BackOutDelegatesToRoutesWhenOverviewHasNothing(t *testing.T) {
	view := Page(workspacePageModel(), nil, nil, nil, context.Background(), nil)
	route := view.RoutesSection.State.Routes[0]
	view.RoutesSection.OpenRoute(route)

	// Overview has nothing to back out; routes has an expanded route.
	view.backOut(tui.KeyEvent{Key: tui.KeyEscape})
	if got := view.RoutesSection.State.ExpandedRoute.Get(); got != "" {
		t.Fatalf("expanded route should close, got %q", got)
	}
}

func TestPage_BackOutDoesNotCrashAtTopLevel(t *testing.T) {
	view := Page(workspacePageModel(), nil, nil, nil, context.Background(), nil)
	// Nothing open in overview or routes; backOut should be a no-op
	// without panicking (it attempts app.Stop but app is nil in this test).
	view.backOut(tui.KeyEvent{Key: tui.KeyEscape})
}

func TestPage_AddTargetRowOpensWorkflow(t *testing.T) {
	commands := fakeWorkspaceCommands{
		listProviders: func(context.Context) ([]readmodel.ProviderOptionReadModel, error) {
			return []readmodel.ProviderOptionReadModel{
				{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
				{ProviderSpec: "openrouter", DisplayName: "OpenRouter", SetupHint: "API key"},
			}, nil
		},
		resolveSetup: func(context.Context, ports.ResolveProviderSetupRequest) (readmodel.ProviderSetupReadModel, error) {
			return readmodel.ProviderSetupReadModel{
				ProviderSpec:    "openai",
				DisplayName:     "OpenAI",
				CredentialLabel: "env:OPENAI_API_KEY",
				CredentialRef:   "env:OPENAI_API_KEY",
				ReadyForCatalog: true,
			}, nil
		},
	}
	view := Page(workspacePageModel(), commands, commands, nil, context.Background(), nil)
	route := readmodel.RouteReadModel{
		ID:        "custom-route",
		ModelName: "custom-route",
		State:     readmodel.RouteNormal,
		Enabled:   true,
	}
	view.RoutesSection.State.Routes = append(view.RoutesSection.State.Routes, route)
	view.RoutesSection.State.ExpandedRoute.Set(route.ID)
	view.RoutesSection.AddTarget(route)

	h, err := testkit.NewHarness(view)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	// The target add workflow renders a provider picker with "provider" title.
	focusUntilFrameContains(t, h, "provider", 20)
	frame := h.Frame()
	expectedOpenAI := "OpenAI · " + profile.ProviderSetupFieldSummaryForSpec("openai")
	expectedOpenRouter := "OpenRouter · " + profile.ProviderSetupFieldSummaryForSpec("openrouter")
	if !strings.Contains(frame, expectedOpenAI) || !strings.Contains(frame, expectedOpenRouter) {
		t.Fatalf("provider picker did not render provider options:\n%s", frame)
	}
	if strings.Contains(frame, "0 of 0 shown") {
		t.Fatalf("provider picker still reports an empty option set:\n%s", frame)
	}

	wf := view.RoutesSection.TargetAddWorkflows[route.ID]
	if wf == nil {
		t.Fatal("expected target add workflow")
	}
	wf.SelectProvider("openai")
	frame = h.Frame()
	if !strings.Contains(frame, "credential") || !strings.Contains(frame, "env:OPENAI_API_KEY") {
		t.Fatalf("provider setup row did not render projected credential:\n%s", frame)
	}
	if !strings.Contains(frame, "loading catalog…") {
		t.Fatalf("provider setup did not advance to loading catalog state:\n%s", frame)
	}
}

func TestPage_DraftWorkspaceEnterSubmitsSlug(t *testing.T) {
	var saved readmodel.WorkspaceReadModel
	view := Page(readmodel.WorkspaceReadModel{State: readmodel.WorkspaceDraft}, fakeWorkspaceCommands{
		save: func(ctx context.Context, request ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
			saved = readmodel.WorkspaceReadModel{
				ID:    readmodel.WorkspaceID(request.Slug),
				Slug:  request.Slug,
				State: readmodel.WorkspaceExisting,
			}
			return saved, nil
		},
	}, nil, nil, context.Background(), nil)

	h, err := testkit.NewHarness(view)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'd'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'e'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'v'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if saved.Slug != "dev" {
		t.Fatalf("draft workspace Enter did not submit slug; focused=%T\nframe:\n%s", h.App().Focused(), h.Frame())
	}

	frame := h.Frame()
	if strings.Contains(frame, "new workspace") {
		t.Fatalf("saved workspace should leave draft create mode:\n%s", frame)
	}
	if !strings.Contains(frame, "model routes") {
		t.Fatalf("saved workspace should render the normal route section:\n%s", frame)
	}
	if !strings.Contains(frame, "activity") {
		t.Fatalf("saved workspace should render the normal activity section:\n%s", frame)
	}
	if !strings.Contains(frame, "add model route") {
		t.Fatalf("saved workspace should expose add model route in the normal body:\n%s", frame)
	}
	testkit.AssertFocusedFrame(t, frame, "> add model route")
}

func TestPage_RequestAddRouteFocusAfterSaveLandsOnAddRouteRow(t *testing.T) {
	view := Page(workspacePageModel(), nil, nil, nil, context.Background(), nil)
	view.RoutesSection.RequestAddRouteFocus()

	h, err := testkit.NewHarness(view)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	testkit.AssertFocusedFrame(t, h.Frame(), "> add model route")
}

func focusUntilFrameContains(t *testing.T, h *testkit.MockAppHarness, needle string, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		if strings.Contains(h.Frame(), needle) {
			return
		}
		h.FocusNext()
	}
	t.Fatalf("frame never contained %q after %d focus steps:\n%s", needle, limit, h.Frame())
}

func findBinding(keymap tui.KeyMap, key tui.Key) (tui.KeyBinding, bool) {
	for _, binding := range keymap {
		if binding.Pattern.Key == key {
			return binding, true
		}
	}
	return tui.KeyBinding{}, false
}

func findRuneBinding(keymap tui.KeyMap, r rune) (tui.KeyBinding, bool) {
	for _, binding := range keymap {
		if binding.Pattern.Key == tui.KeyRune && binding.Pattern.Rune == r {
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
				Enabled:   true,
				Targets: []readmodel.TargetReadModel{
					{ID: "local-1", Provider: "ollama", Model: "llama3.2", Rank: 1},
				},
			},
		},
	}
}

type fakeWorkspaceCommands struct {
	save          func(context.Context, ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error)
	listProviders func(context.Context) ([]readmodel.ProviderOptionReadModel, error)
	resolveSetup  func(context.Context, ports.ResolveProviderSetupRequest) (readmodel.ProviderSetupReadModel, error)
}

func (f fakeWorkspaceCommands) SaveWorkspace(ctx context.Context, request ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
	if f.save != nil {
		return f.save(ctx, request)
	}
	return readmodel.WorkspaceReadModel{}, nil
}

func (f fakeWorkspaceCommands) DeleteWorkspace(context.Context, ports.DeleteWorkspaceRequest) error {
	return nil
}

func (f fakeWorkspaceCommands) ListTargetProviders(ctx context.Context) ([]readmodel.ProviderOptionReadModel, error) {
	if f.listProviders != nil {
		return f.listProviders(ctx)
	}
	return nil, nil
}

func (f fakeWorkspaceCommands) ResolveProviderSetup(ctx context.Context, req ports.ResolveProviderSetupRequest) (readmodel.ProviderSetupReadModel, error) {
	if f.resolveSetup != nil {
		return f.resolveSetup(ctx, req)
	}
	return readmodel.ProviderSetupReadModel{}, nil
}

func (f fakeWorkspaceCommands) ProbeProviderModels(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
	return readmodel.ModelCatalogReadModel{}, nil
}
