package cockpit

import (
	"context"
	"errors"
	"reflect"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	overviewsection "github.com/swobuforge/swobu/internal/cockpit/sections/workspace_overview"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func NewCockpitWithWorkspacePorts(model readmodel.CockpitReadModel, query ports.WorkspaceQueries, commands ports.WorkspaceCommands) *Cockpit {
	return NewCockpitWithContext(model, context.Background(), query, commands)
}

func TestCockpit_KeyMapOwnsGlobalNavigationOnly(t *testing.T) {
	cockpit := NewCockpit(DefaultFixtureReadModel())
	keymap := cockpit.KeyMap()

	for _, event := range []tui.KeyEvent{
		{Key: tui.KeyTab},
		{Key: tui.KeyTab, Mod: tui.ModShift},
		{Key: tui.KeyRune, Rune: 'q'},
	} {
		binding, ok := findRootBinding(keymap, event)
		if !ok {
			t.Fatalf("missing root binding for %#v", event)
		}
		if !binding.Stop {
			t.Fatalf("root binding for %#v should stop propagation", event)
		}
		if binding.Pattern.FocusRequired {
			t.Fatalf("root binding for %#v should not require focus", event)
		}
	}

	binding, ok := findRootBinding(keymap, tui.KeyEvent{Key: tui.KeyF1})
	if !ok {
		t.Fatal("missing root binding for help hotkey")
	}
	if !binding.Stop {
		t.Fatal("help hotkey should stop propagation")
	}
	if binding.Pattern.FocusRequired {
		t.Fatal("help hotkey should not require focus")
	}

	for _, key := range []tui.Key{tui.KeyUp, tui.KeyDown, tui.KeyEnter, tui.KeyEscape} {
		if _, ok := findRootBinding(keymap, tui.KeyEvent{Key: key}); ok {
			t.Fatalf("root should not own surface key %v", key)
		}
	}
}

func TestCockpit_F1ActivatesHelpWorld(t *testing.T) {
	cockpit := NewCockpit(DefaultFixtureReadModel())

	pressRootKey(t, cockpit, tui.KeyEvent{Key: tui.KeyF1})
	assertActiveTab(t, cockpit, "?", readmodel.CockpitHelpPage)
	assertRenderContains(t, cockpit, "[› ?]", "help")
}

func TestCockpit_ShiftTabNavigationWrapsBackward(t *testing.T) {
	cockpit := NewCockpit(DefaultFixtureReadModel())

	pressRootKey(t, cockpit, tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift})
	assertActiveTab(t, cockpit, "?", readmodel.CockpitHelpPage)
	assertRenderContains(t, cockpit, "[› ?]", "help")
}

func TestApplyCockpitDefaultsInstallsHelpCopy(t *testing.T) {
	model := applyCockpitDefaults(readmodel.CockpitReadModel{})
	if got, want := model.Help.DocsURL, "swobu.com/docs"; got != want {
		t.Fatalf("docs url = %q, want %q", got, want)
	}
	if got, want := model.Help.CommunityURL, "https://discord.gg/swobu"; got != want {
		t.Fatalf("community url = %q, want %q", got, want)
	}
	if got, want := model.Help.IssueURL, "https://github.com/swobuforge/swobu/issues/new"; got != want {
		t.Fatalf("issue url = %q, want %q", got, want)
	}
}

func TestCockpit_WorkspaceSaveRefreshesAndSelectsSavedWorkspace(t *testing.T) {
	fake := &fakeWorkspacePorts{
		cockpit: readmodel.CockpitReadModel{
			HeaderRight:         "test",
			ActivePage:          readmodel.CockpitWorkspacePage,
			SelectedWorkspaceID: "lab",
			SelectedWorkspace: readmodel.WorkspaceReadModel{
				ID:            "lab",
				Slug:          "lab",
				State:         readmodel.WorkspaceExisting,
				ClientBaseURL: "http://127.0.0.1:7926/c/lab",
			},
			Tabs: []readmodel.WorkspaceTabReadModel{
				{ID: "lab", Slug: "lab", Kind: readmodel.WorkspaceTabExisting, Selected: true},
				{ID: "prod", Slug: "prod", Kind: readmodel.WorkspaceTabExisting},
				{ID: "+", Kind: readmodel.WorkspaceTabDraft},
				{ID: "?", Kind: readmodel.WorkspaceTabHelp},
			},
		},
		workspaces: map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel{
			"prod": {
				ID:            "prod",
				Slug:          "prod",
				State:         readmodel.WorkspaceExisting,
				ClientBaseURL: "http://127.0.0.1:7926/c/prod",
				Routes: []readmodel.RouteReadModel{{
					ID:        "gpt-4.1",
					ModelName: "gpt-4.1",
					State:     readmodel.RouteNormal,
					Enabled:   true,
					Targets:   []readmodel.TargetReadModel{{ID: "target-1"}},
				}},
			},
		},
	}
	cockpit := NewCockpitWithWorkspacePorts(DefaultFixtureReadModel(), fake, fake)

	cockpit.currentWorkspacePage().OnWorkspaceSaved(readmodel.WorkspaceReadModel{ID: "prod", Slug: "prod"})

	assertActiveTab(t, cockpit, "prod", readmodel.CockpitWorkspacePage)
	assertNoRefreshNotice(t, cockpit)
	assertRenderContains(t, cockpit, "[› prod]", "http://127.0.0.1:7926/c/prod", "gpt-4.1")
	if fake.loadCockpitCalls != 1 || fake.loadWorkspaceCalls != 1 {
		t.Fatalf("refresh calls = cockpit %d workspace %d, want 1/1", fake.loadCockpitCalls, fake.loadWorkspaceCalls)
	}
}

func TestCockpit_WorkspaceSaveRefreshPreservesDraftPage(t *testing.T) {
	fake := &fakeWorkspacePorts{
		cockpit: readmodel.CockpitReadModel{
			HeaderRight:         "test",
			ActivePage:          readmodel.CockpitWorkspacePage,
			SelectedWorkspaceID: "prod",
			SelectedWorkspace: readmodel.WorkspaceReadModel{
				ID:            "prod",
				Slug:          "prod",
				State:         readmodel.WorkspaceExisting,
				ClientBaseURL: "http://127.0.0.1:7926/c/prod",
			},
			Tabs: []readmodel.WorkspaceTabReadModel{
				{ID: "prod", Slug: "prod", Kind: readmodel.WorkspaceTabExisting, Selected: true},
				{ID: "+", Kind: readmodel.WorkspaceTabDraft},
				{ID: "?", Kind: readmodel.WorkspaceTabHelp},
			},
		},
		workspaces: map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel{
			"prod": {
				ID:            "prod",
				Slug:          "prod",
				State:         readmodel.WorkspaceExisting,
				ClientBaseURL: "http://127.0.0.1:7926/c/prod",
			},
		},
	}
	cockpit := NewCockpitWithWorkspacePorts(DefaultFixtureReadModel(), fake, fake)
	draftPage := cockpit.WorkspacePages["+"]
	if draftPage == nil {
		t.Fatal("draft page missing before refresh")
	}

	cockpit.currentWorkspacePage().OnWorkspaceSaved(readmodel.WorkspaceReadModel{ID: "prod", Slug: "prod"})

	if got := cockpit.WorkspacePages["+"]; got != draftPage {
		t.Fatalf("draft page should be preserved across refresh")
	}
	assertActiveTab(t, cockpit, "prod", readmodel.CockpitWorkspacePage)
}

func TestCockpit_WorkspaceSaveRefreshRemountsPromotedDraftPage(t *testing.T) {
	cockpit := NewCockpitWithWorkspacePorts(readmodel.CockpitReadModel{
		HeaderRight:         "test",
		ActivePage:          readmodel.CockpitWorkspacePage,
		SelectedWorkspaceID: "+",
		SelectedWorkspace: readmodel.WorkspaceReadModel{
			ID:    "+",
			State: readmodel.WorkspaceDraft,
		},
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "+", Kind: readmodel.WorkspaceTabDraft, Selected: true},
			{ID: "lab", Slug: "lab", Kind: readmodel.WorkspaceTabExisting},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
	}, nil, nil)
	draftPage := cockpit.WorkspacePages["+"]
	if draftPage == nil {
		t.Fatal("draft page missing before promotion")
	}

	cockpit.currentWorkspacePage().OnWorkspaceSaved(readmodel.WorkspaceReadModel{
		ID:            "lab",
		Slug:          "lab",
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/lab",
	})

	promotedDraftPage := cockpit.WorkspacePages["+"]
	if promotedDraftPage == nil {
		t.Fatal("draft page missing after promotion")
	}
	if promotedDraftPage == draftPage {
		t.Fatal("promoted draft page should remount as a fresh draft page")
	}
	if !promotedDraftPage.OverviewSection.Model.IsDraft() {
		t.Fatalf("draft page model = %#v, want draft", promotedDraftPage.OverviewSection.Model)
	}
	if promotedDraftPage.OverviewSection.Model.Slug != "" {
		t.Fatalf("draft page slug = %q, want empty", promotedDraftPage.OverviewSection.Model.Slug)
	}
}

func TestCockpit_WorkspaceSaveRefreshPreservesActiveRouteEditor(t *testing.T) {
	fake := &fakeWorkspacePorts{
		cockpit:          DefaultFixtureReadModel(),
		loadWorkspaceErr: errors.New("workspace stale"),
	}
	cockpit := NewCockpitWithWorkspacePorts(DefaultFixtureReadModel(), fake, fake)

	page := cockpit.currentWorkspacePage()
	route := page.RoutesSection.State.Routes[0]
	page.RoutesSection.OpenRoute(route)
	page.RoutesSection.OpenTargetEditor(route, route.Targets[0])

	page.OnWorkspaceSaved(readmodel.WorkspaceReadModel{
		ID:            "prod",
		Slug:          "prod",
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/prod",
	})

	assertActiveTab(t, cockpit, "prod", readmodel.CockpitWorkspacePage)
	if got := cockpit.currentWorkspacePage().RoutesSection.State.ExpandedRoute.Get(); got != "gpt" {
		t.Fatalf("expanded route = %q, want gpt", got)
	}
	if got := cockpit.currentWorkspacePage().RoutesSection.State.OpenTarget.Get(); got != "target-1" {
		t.Fatalf("opened target = %q, want target-1", got)
	}
}

func TestCockpit_WorkspaceSaveRefreshFailureShowsStaleNotice(t *testing.T) {
	fake := &fakeWorkspacePorts{
		loadCockpitErr: errors.New("daemon offline"),
	}
	cockpit := NewCockpitWithWorkspacePorts(DefaultFixtureReadModel(), fake, fake)

	cockpit.currentWorkspacePage().OnWorkspaceSaved(readmodel.WorkspaceReadModel{
		ID:            "prod",
		Slug:          "prod",
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/prod",
	})

	assertActiveTab(t, cockpit, "prod", readmodel.CockpitWorkspacePage)
	assertRefreshNotice(t, cockpit, readmodel.NoticeStale, "refresh stale: saved workspace shown; daemon offline")
	assertRenderContains(t, cockpit, "refresh stale: saved workspace shown; daemon offline")
}

func TestCockpit_WorkspaceSaveRefreshLoadWorkspaceFailureUsesSavedModelAndShowsNotice(t *testing.T) {
	fake := &fakeWorkspacePorts{
		cockpit: readmodel.CockpitReadModel{
			HeaderRight:         "test",
			ActivePage:          readmodel.CockpitWorkspacePage,
			SelectedWorkspaceID: "lab",
			SelectedWorkspace:   readmodel.WorkspaceReadModel{ID: "lab", Slug: "lab", State: readmodel.WorkspaceExisting},
			Tabs: []readmodel.WorkspaceTabReadModel{
				{ID: "lab", Slug: "lab", Kind: readmodel.WorkspaceTabExisting, Selected: true},
				{ID: "prod", Slug: "prod", Kind: readmodel.WorkspaceTabExisting},
				{ID: "?", Kind: readmodel.WorkspaceTabHelp},
			},
		},
		loadWorkspaceErr: errors.New("workspace stale"),
	}
	cockpit := NewCockpitWithWorkspacePorts(DefaultFixtureReadModel(), fake, fake)

	cockpit.currentWorkspacePage().OnWorkspaceSaved(readmodel.WorkspaceReadModel{
		ID:            "prod",
		Slug:          "prod",
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/prod",
	})

	assertActiveTab(t, cockpit, "prod", readmodel.CockpitWorkspacePage)
	assertRefreshNotice(t, cockpit, readmodel.NoticeStale, "refresh stale: saved workspace shown; workspace stale")
	assertRenderContains(t, cockpit, "refresh stale: saved workspace shown; workspace stale", "http://127.0.0.1:7926/c/prod")
}

func TestCockpit_WorkspaceDeleteRefreshesAndSelectsRemainingWorkspace(t *testing.T) {
	fake := &fakeWorkspacePorts{
		cockpit: readmodel.CockpitReadModel{
			HeaderRight:         "test",
			ActivePage:          readmodel.CockpitWorkspacePage,
			SelectedWorkspaceID: "lab",
			SelectedWorkspace: readmodel.WorkspaceReadModel{
				ID:            "lab",
				Slug:          "lab",
				State:         readmodel.WorkspaceExisting,
				ClientBaseURL: "http://127.0.0.1:7926/c/lab",
			},
			Tabs: []readmodel.WorkspaceTabReadModel{
				{ID: "lab", Slug: "lab", Kind: readmodel.WorkspaceTabExisting, Selected: true},
				{ID: "+", Kind: readmodel.WorkspaceTabDraft},
				{ID: "?", Kind: readmodel.WorkspaceTabHelp},
			},
		},
	}
	cockpit := NewCockpitWithWorkspacePorts(DefaultFixtureReadModel(), fake, fake)

	cockpit.currentWorkspacePage().OnWorkspaceDeleted("dev")

	assertActiveTab(t, cockpit, "lab", readmodel.CockpitWorkspacePage)
	assertNoRefreshNotice(t, cockpit)
	assertRenderContains(t, cockpit, "[› lab]", "http://127.0.0.1:7926/c/lab")
	if fake.loadCockpitCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", fake.loadCockpitCalls)
	}
}

func TestCockpit_WorkspaceDeleteConfirmationRefreshesThroughCommandPath(t *testing.T) {
	fake := &fakeWorkspacePorts{
		cockpit: readmodel.CockpitReadModel{
			HeaderRight:         "test",
			ActivePage:          readmodel.CockpitWorkspacePage,
			SelectedWorkspaceID: "lab",
			SelectedWorkspace: readmodel.WorkspaceReadModel{
				ID:            "lab",
				Slug:          "lab",
				State:         readmodel.WorkspaceExisting,
				ClientBaseURL: "http://127.0.0.1:7926/c/lab",
			},
			Tabs: []readmodel.WorkspaceTabReadModel{
				{ID: "lab", Slug: "lab", Kind: readmodel.WorkspaceTabExisting, Selected: true},
				{ID: "?", Kind: readmodel.WorkspaceTabHelp},
			},
		},
	}
	cockpit := NewCockpitWithWorkspacePorts(DefaultFixtureReadModel(), fake, fake)

	confirmation := overviewsection.DeleteConfirmation(cockpit.currentWorkspacePage().OverviewSection)
	confirmation.Request("dev")
	confirmation.Confirm(context.Background())

	if fake.deleteWorkspaceCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", fake.deleteWorkspaceCalls)
	}
	if fake.deletedWorkspaceID != "dev" {
		t.Fatalf("deleted workspace = %q, want dev", fake.deletedWorkspaceID)
	}
	if fake.loadCockpitCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", fake.loadCockpitCalls)
	}
	assertActiveTab(t, cockpit, "lab", readmodel.CockpitWorkspacePage)
}

func TestCockpit_WorkspaceDeleteRefreshSuccessHidesDeletedWorkspaceWhenProjectionIsStale(t *testing.T) {
	fake := &fakeWorkspacePorts{
		cockpit: DefaultFixtureReadModel(),
	}
	cockpit := NewCockpitWithWorkspacePorts(DefaultFixtureReadModel(), fake, fake)

	cockpit.currentWorkspacePage().OnWorkspaceDeleted("dev")

	assertActiveTab(t, cockpit, "lab", readmodel.CockpitWorkspacePage)
	got := testkit.RenderMountedString(t, cockpit, 100, 24)
	testkit.AssertNow(t, got, testkit.All(
		testkit.Not(testkit.Text("[dev]").Exists()),
		testkit.Not(testkit.Text("[› dev]").Exists()),
	))
}

func TestCockpit_WorkspaceDeleteRefreshFailureHidesDeletedWorkspace(t *testing.T) {
	fake := &fakeWorkspacePorts{
		loadCockpitErr: errors.New("daemon offline"),
	}
	cockpit := NewCockpitWithWorkspacePorts(DefaultFixtureReadModel(), fake, fake)

	cockpit.currentWorkspacePage().OnWorkspaceDeleted("dev")

	assertActiveTab(t, cockpit, "lab", readmodel.CockpitWorkspacePage)
	assertRefreshNotice(t, cockpit, readmodel.NoticeStale, "refresh stale: deleted workspace hidden; daemon offline")
	got := testkit.RenderMountedString(t, cockpit, 100, 24)
	testkit.AssertNow(t, got, testkit.All(
		testkit.Text("refresh stale: deleted workspace hidden; daemon offline").Exists(),
		testkit.Text("[› lab]").Exists(),
		testkit.Not(testkit.Text("[dev]").Exists()),
		testkit.Not(testkit.Text("[› dev]").Exists()),
	))
}

func TestCockpit_RemoveLastWorkspaceActivatesDraftTab(t *testing.T) {
	model := readmodel.CockpitReadModel{
		ActivePage:          readmodel.CockpitWorkspacePage,
		SelectedWorkspaceID: "dev",
		SelectedWorkspace:   readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting},
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "dev", Slug: "dev", Kind: readmodel.WorkspaceTabExisting, Selected: true},
			{ID: "+", Kind: readmodel.WorkspaceTabDraft},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
	}

	got := removeWorkspaceFromModel(model, "dev")

	if got.ActivePage != readmodel.CockpitWorkspacePage {
		t.Fatalf("active page = %v, want workspace", got.ActivePage)
	}
	if got.SelectedWorkspaceID != "+" {
		t.Fatalf("selected workspace = %q, want draft", got.SelectedWorkspaceID)
	}
	if index, ok := draftTabIndex(got.Tabs); !ok || !got.Tabs[index].Selected {
		t.Fatalf("draft tab should be selected: %#v", got.Tabs)
	}
	if !got.SelectedWorkspace.IsDraft() {
		t.Fatalf("selected workspace = %#v, want draft workspace", got.SelectedWorkspace)
	}
}

func TestCockpit_SelectedExistingTabDoesNotReuseDraftWorkspaceBody(t *testing.T) {
	cockpit := NewCockpit(readmodel.CockpitReadModel{
		ActivePage:          readmodel.CockpitWorkspacePage,
		SelectedWorkspaceID: "+",
		SelectedWorkspace:   readmodel.WorkspaceReadModel{ID: "+", State: readmodel.WorkspaceDraft},
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "dev", Slug: "dev", Kind: readmodel.WorkspaceTabExisting},
			{ID: "+", Kind: readmodel.WorkspaceTabDraft, Selected: true},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
	})
	cockpit.ActiveTabIndex.Set(0)

	model := cockpit.activeModel()
	if model.SelectedWorkspaceID != "dev" {
		t.Fatalf("selected workspace = %q, want dev", model.SelectedWorkspaceID)
	}
	if model.SelectedWorkspace.IsDraft() {
		t.Fatalf("selected workspace = %#v, want existing workspace", model.SelectedWorkspace)
	}

	got := testkit.RenderMountedString(t, cockpit, 100, 24)
	testkit.AssertNow(t, got, testkit.All(
		testkit.Text("[› dev]").Exists(),
		testkit.Text("edit ↵").Exists(),
		testkit.Not(testkit.Text("create ↵").Exists()),
	))
}

func TestCockpit_WorkspaceRefreshUsesCockpitContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &fakeWorkspacePorts{}
	cockpit := NewCockpitWithContext(DefaultFixtureReadModel(), ctx, fake, fake)

	cockpit.currentWorkspacePage().OnWorkspaceSaved(readmodel.WorkspaceReadModel{ID: "prod", Slug: "prod"})

	if fake.loadCockpitCtxErr == nil || !errors.Is(fake.loadCockpitCtxErr, context.Canceled) {
		t.Fatalf("load cockpit context error = %v, want canceled", fake.loadCockpitCtxErr)
	}
}

func TestCockpit_QuitShortcutIsRootFallback(t *testing.T) {
	cockpit := NewCockpit(DefaultFixtureReadModel())
	binding, ok := findRootBinding(cockpit.KeyMap(), tui.KeyEvent{Key: tui.KeyRune, Rune: 'q'})
	if !ok {
		t.Fatal("missing q binding")
	}
	if !binding.Stop {
		t.Fatal("q binding should stop propagation")
	}

	binding.Handler(tui.KeyEvent{Key: tui.KeyRune, Rune: 'q'})
}

func TestCockpit_DoesNotRetainSeparateWorkspacePageOwner(t *testing.T) {
	cockpitType := reflect.TypeOf(Cockpit{})
	if _, ok := cockpitType.FieldByName("WorkspacePage"); ok {
		t.Fatal("Cockpit must not retain separate WorkspacePage owner; use currentWorkspacePage() over WorkspacePages only")
	}
}

type fakeWorkspacePorts struct {
	cockpit              readmodel.CockpitReadModel
	workspaces           map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel
	loadCockpitErr       error
	loadWorkspaceErr     error
	loadCockpitCalls     int
	loadWorkspaceCalls   int
	deleteWorkspaceCalls int
	deletedWorkspaceID   readmodel.WorkspaceID
	loadCockpitCtxErr    error
	loadWorkspaceCtxErr  error
}

func (f *fakeWorkspacePorts) LoadCockpit(ctx context.Context) (readmodel.CockpitReadModel, error) {
	f.loadCockpitCalls++
	f.loadCockpitCtxErr = ctx.Err()
	if f.loadCockpitErr != nil {
		return readmodel.CockpitReadModel{}, f.loadCockpitErr
	}
	if err := ctx.Err(); err != nil {
		return readmodel.CockpitReadModel{}, err
	}
	return f.cockpit, nil
}

func (f *fakeWorkspacePorts) LoadWorkspace(ctx context.Context, id readmodel.WorkspaceID) (readmodel.WorkspaceReadModel, error) {
	f.loadWorkspaceCalls++
	f.loadWorkspaceCtxErr = ctx.Err()
	if f.loadWorkspaceErr != nil {
		return readmodel.WorkspaceReadModel{}, f.loadWorkspaceErr
	}
	if err := ctx.Err(); err != nil {
		return readmodel.WorkspaceReadModel{}, err
	}
	return f.workspaces[id], nil
}

func (f *fakeWorkspacePorts) SaveWorkspace(context.Context, ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
	return readmodel.WorkspaceReadModel{}, nil
}

func (f *fakeWorkspacePorts) DeleteWorkspace(_ context.Context, request ports.DeleteWorkspaceRequest) error {
	f.deleteWorkspaceCalls++
	f.deletedWorkspaceID = request.ID
	return nil
}

func pressRootKey(t *testing.T, cockpit *Cockpit, event tui.KeyEvent) {
	t.Helper()
	for _, binding := range cockpit.KeyMap() {
		if rootBindingMatches(binding.Pattern, event) {
			binding.Handler(event)
			return
		}
	}
	t.Fatalf("no root binding matched %#v", event)
}

func assertActiveTab(t *testing.T, cockpit *Cockpit, wantID readmodel.WorkspaceID, wantPage readmodel.CockpitPage) {
	t.Helper()
	model := cockpit.activeModel()
	if model.SelectedWorkspaceID != wantID {
		t.Fatalf("selected tab = %q, want %q", model.SelectedWorkspaceID, wantID)
	}
	if model.ActivePage != wantPage {
		t.Fatalf("active page = %v, want %v", model.ActivePage, wantPage)
	}
}

func assertRenderContains(t *testing.T, cockpit *Cockpit, values ...string) {
	t.Helper()
	got := testkit.RenderMountedString(t, cockpit, 100, 24)
	preds := make([]testkit.Predicate, 0, len(values))
	for _, value := range values {
		preds = append(preds, testkit.Text(value).Exists())
	}
	testkit.AssertNow(t, got, testkit.All(preds...))
}

func assertRefreshNotice(t *testing.T, cockpit *Cockpit, kind readmodel.NoticeKind, message string) {
	t.Helper()
	if cockpit.RefreshNotice == nil {
		t.Fatal("refresh notice state is nil")
	}
	got := cockpit.RefreshNotice.Get()
	if got.Kind != kind || got.Message != message {
		t.Fatalf("refresh notice = %#v, want kind %v message %q", got, kind, message)
	}
}

func assertNoRefreshNotice(t *testing.T, cockpit *Cockpit) {
	t.Helper()
	if cockpit.RefreshNotice == nil {
		t.Fatal("refresh notice state is nil")
	}
	if got := cockpit.RefreshNotice.Get(); !got.IsEmpty() {
		t.Fatalf("refresh notice = %#v, want empty", got)
	}
}

func findRootBinding(keymap tui.KeyMap, event tui.KeyEvent) (tui.KeyBinding, bool) {
	for _, binding := range keymap {
		if rootBindingMatches(binding.Pattern, event) {
			return binding, true
		}
	}
	return tui.KeyBinding{}, false
}

func rootBindingMatches(pattern tui.KeyPattern, event tui.KeyEvent) bool {
	if pattern.ExcludeMods != 0 && event.Mod&pattern.ExcludeMods != 0 {
		return false
	}
	if pattern.Mod != 0 && event.Mod != pattern.Mod {
		return false
	}
	if pattern.Rune != 0 {
		return event.Key == tui.KeyRune && event.Rune == pattern.Rune
	}
	return pattern.Key != 0 && event.Key == pattern.Key
}
