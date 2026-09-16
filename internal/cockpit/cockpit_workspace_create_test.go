package cockpit

import (
	"context"
	"errors"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/app/operator/shares"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	routessection "github.com/swobuforge/swobu/internal/cockpit/sections/routes"
	overviewsection "github.com/swobuforge/swobu/internal/cockpit/sections/workspace_overview"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/sharestate"
	"github.com/swobuforge/swobu/internal/testkit/cockpittestkit"
)

type workspaceCreateCommands struct {
	saved       ports.RenameWorkspaceRequest
	saveCalls   int
	deleteCalls int
	revealShare func(string) (shares.Result, error)
}

func (c *workspaceCreateCommands) IssueShare(context.Context, string, sharestate.Expiry) (shares.Result, error) {
	return shares.Result{}, nil
}

func (c *workspaceCreateCommands) RevealShare(_ context.Context, ref string) (shares.Result, error) {
	if c.revealShare != nil {
		return c.revealShare(ref)
	}
	return shares.Result{}, nil
}

func (c *workspaceCreateCommands) RevokeShare(context.Context, string) error { return nil }

func TestCockpit_PersistedWorkspaceSwitchUsesRegistryProjection(t *testing.T) {
	workspaceA := readmodel.WorkspaceReadModel{ID: "a", Slug: "a", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{{ID: "route-a", ModelName: "route-a"}}}
	workspaceB := readmodel.WorkspaceReadModel{ID: "b", Slug: "b", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{{ID: "route-b", ModelName: "route-b"}}}
	root := NewCockpit(readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "a", Slug: "a", Kind: readmodel.WorkspaceTabExisting, Selected: true}, {ID: "b", Slug: "b", Kind: readmodel.WorkspaceTabExisting}, {ID: "?", Kind: readmodel.WorkspaceTabHelp}},
		SelectedWorkspaceID: "a", SelectedWorkspace: workspaceA,
		Workspaces: map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel{"a": workspaceA, "b": workspaceB},
	})
	if got := root.activeModel().SelectedWorkspace.Routes[0].ModelName; got != "route-a" {
		t.Fatalf("initial workspace route = %q", got)
	}
	root.ActiveTabIndex.Set(1)
	if got := root.activeModel().SelectedWorkspace.Routes[0].ModelName; got != "route-b" {
		t.Fatalf("switched workspace route = %q, want route-b", got)
	}
	root.ActiveTabIndex.Set(0)
	if got := root.activeModel().SelectedWorkspace.Routes[0].ModelName; got != "route-a" {
		t.Fatalf("returned workspace route = %q, want route-a", got)
	}
}

func TestCockpit_CommittedWorkspaceSurvivesSwitchAndRemount(t *testing.T) {
	a := readmodel.WorkspaceReadModel{ID: "a", Slug: "a", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{{ID: "old", ModelName: "old"}}}
	b := readmodel.WorkspaceReadModel{ID: "b", Slug: "b", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{{ID: "b-route", ModelName: "b-route"}}}
	root := NewCockpit(readmodel.CockpitReadModel{Tabs: []readmodel.WorkspaceTabReadModel{{ID: "a", Slug: "a", Kind: readmodel.WorkspaceTabExisting, Selected: true}, {ID: "b", Slug: "b", Kind: readmodel.WorkspaceTabExisting}}, SelectedWorkspaceID: "a", SelectedWorkspace: a, Workspaces: map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel{"a": a, "b": b}})
	committed := a
	committed.Routes = []readmodel.RouteReadModel{{ID: "new", ModelName: "committed"}}
	root.currentWorkspacePage().OnWorkspaceCommitted(committed)
	root.replaceModel(root.activeModel(), false)
	root.ActiveTabIndex.Set(1)
	root.ActiveTabIndex.Set(0)
	if got := root.currentWorkspacePage().RoutesSection.State.Routes[0].ModelName; got != "committed" {
		t.Fatalf("returned route = %q, want committed", got)
	}
}

func TestCockpit_DiscardNamedDraftIsLocal(t *testing.T) {
	model := readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "+", Slug: "buildweek", Kind: readmodel.WorkspaceTabDraft, Selected: true}, {ID: "?", Kind: readmodel.WorkspaceTabHelp}},
		SelectedWorkspaceID: "+",
		SelectedWorkspace:   readmodel.WorkspaceReadModel{ID: "+", Slug: "buildweek", State: readmodel.WorkspaceDraft},
		ActivePage:          readmodel.CockpitWorkspacePage,
	}
	commands := &workspaceCreateCommands{}
	root := NewCockpitWithContext(model, context.Background(), &workspaceCreateQueries{}, commands)
	page := root.currentWorkspacePage()
	row := overviewsection.DraftDiscardComponent(page.OverviewSection)
	row.OpenConfirm()
	row.Confirm()

	active := root.activeModel()
	if active.SelectedWorkspace.ID != "+" || active.SelectedWorkspace.Slug != "" || !active.SelectedWorkspace.IsDraft() {
		t.Fatalf("discarded draft = %#v", active.SelectedWorkspace)
	}
	if commands.deleteCalls != 0 {
		t.Fatalf("discard called daemon delete %d times", commands.deleteCalls)
	}
}

type workspaceCreateQueries struct {
	loadCockpitCalls   int
	loadWorkspaceCalls int
	activityCalls      int
	loadErr            error
	loadModel          readmodel.CockpitReadModel
	loadWorkspace      readmodel.WorkspaceReadModel
}

func (q *workspaceCreateQueries) LoadCockpit(context.Context) (readmodel.CockpitReadModel, error) {
	q.loadCockpitCalls++
	if q.loadErr != nil {
		return readmodel.CockpitReadModel{}, q.loadErr
	}
	if len(q.loadModel.Tabs) > 0 {
		return q.loadModel, nil
	}
	return readmodel.CockpitReadModel{}, errors.New("draft workspace is not persisted yet")
}

func TestCockpit_SaveRefreshFailureIsWorkspaceLocal(t *testing.T) {
	model := readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "personal", Slug: "personal", Kind: readmodel.WorkspaceTabExisting, Selected: true}},
		SelectedWorkspaceID: "personal", SelectedWorkspace: readmodel.WorkspaceReadModel{ID: "personal", Slug: "personal", State: readmodel.WorkspaceExisting}, ActivePage: readmodel.CockpitWorkspacePage,
	}
	root := NewCockpitWithContext(model, context.Background(), &workspaceCreateQueries{loadErr: errors.New("daemon unavailable")}, &workspaceCreateCommands{})
	root.refreshAfterWorkspaceSave(readmodel.WorkspaceReadModel{ID: "personal", Slug: "renamed", State: readmodel.WorkspaceExisting})
	if root.RefreshWarning.Get() != "" {
		t.Fatalf("save refresh escaped to root: %q", root.RefreshWarning.Get())
	}
	if got := root.currentWorkspacePage().OverviewSection.WorkspaceWarning.Get(); !strings.Contains(got, "saved workspace shown") {
		t.Fatalf("workspace warning = %q", got)
	}
}

func TestCockpit_DeleteRefreshFailureUsesRootRefreshWarning(t *testing.T) {
	model := readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "personal", Slug: "personal", Kind: readmodel.WorkspaceTabExisting, Selected: true}},
		SelectedWorkspaceID: "personal", SelectedWorkspace: readmodel.WorkspaceReadModel{ID: "personal", Slug: "personal", State: readmodel.WorkspaceExisting}, ActivePage: readmodel.CockpitWorkspacePage,
	}
	root := NewCockpitWithContext(model, context.Background(), &workspaceCreateQueries{loadErr: errors.New("daemon unavailable")}, &workspaceCreateCommands{})
	root.refreshAfterWorkspaceDelete("personal")
	if got := root.RefreshWarning.Get(); got != "workspace deleted · refresh unavailable; local view reconciled" {
		t.Fatalf("root refresh warning = %q", got)
	}
	frame := testkit.RenderMountedTrimmed(t, root, 100, 24)
	if !strings.Contains(frame, "workspace deleted · refresh unavailable; local view reconciled") {
		t.Fatalf("root refresh warning not rendered:\n%s", frame)
	}
}

func TestCockpit_SuccessfulRootRefreshClearsRootWarning(t *testing.T) {
	workspace := readmodel.WorkspaceReadModel{ID: "personal", Slug: "personal", State: readmodel.WorkspaceExisting}
	model := readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "personal", Slug: "personal", Kind: readmodel.WorkspaceTabExisting, Selected: true}},
		SelectedWorkspaceID: "personal", SelectedWorkspace: workspace, ActivePage: readmodel.CockpitWorkspacePage,
	}
	queries := &workspaceCreateQueries{loadModel: model, loadWorkspace: workspace}
	root := NewCockpitWithContext(model, context.Background(), queries, &workspaceCreateCommands{})
	root.RefreshWarning.Set("workspace deleted · refresh unavailable; local view reconciled")

	root.refreshAfterWorkspaceSave(workspace)

	if got := root.RefreshWarning.Get(); got != "" {
		t.Fatalf("authoritative save refresh retained root warning %q", got)
	}
}

func TestCockpit_FreshRootWithWorkspaceDetailFailureClearsRootWarning(t *testing.T) {
	workspace := readmodel.WorkspaceReadModel{ID: "personal", Slug: "personal", State: readmodel.WorkspaceExisting}
	model := readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "personal", Slug: "personal", Kind: readmodel.WorkspaceTabExisting, Selected: true}},
		SelectedWorkspaceID: "personal", SelectedWorkspace: workspace, ActivePage: readmodel.CockpitWorkspacePage,
	}
	queries := &workspaceCreateQueries{loadModel: model}
	root := NewCockpitWithContext(model, context.Background(), queries, &workspaceCreateCommands{})
	root.RefreshWarning.Set("workspace deleted · refresh unavailable; local view reconciled")

	root.refreshAfterWorkspaceSave(workspace)

	if got := root.RefreshWarning.Get(); got != "" {
		t.Fatalf("fresh root retained stale warning %q", got)
	}
	if got := root.currentWorkspacePage().OverviewSection.WorkspaceWarning.Get(); !strings.Contains(got, "saved workspace shown") {
		t.Fatalf("workspace detail failure was not local: %q", got)
	}
}

func TestCockpit_LocalSaveProjectionDoesNotClearRootWarning(t *testing.T) {
	workspace := readmodel.WorkspaceReadModel{ID: "personal", Slug: "personal", State: readmodel.WorkspaceExisting}
	model := readmodel.CockpitReadModel{Tabs: []readmodel.WorkspaceTabReadModel{{ID: "personal", Slug: "personal", Kind: readmodel.WorkspaceTabExisting, Selected: true}}, SelectedWorkspaceID: "personal", SelectedWorkspace: workspace}
	root := NewCockpitWithContext(model, context.Background(), nil, &workspaceCreateCommands{})
	root.RefreshWarning.Set("workspace deleted · refresh unavailable; local view reconciled")

	root.refreshAfterWorkspaceSave(workspace)

	if got := root.RefreshWarning.Get(); got == "" {
		t.Fatal("local save projection cleared root freshness warning")
	}
}

func (q *workspaceCreateQueries) LoadWorkspace(context.Context, readmodel.WorkspaceID) (readmodel.WorkspaceReadModel, error) {
	q.loadWorkspaceCalls++
	if q.loadWorkspace.ID != "" {
		return q.loadWorkspace, nil
	}
	return readmodel.WorkspaceReadModel{}, errors.New("draft workspace is not persisted yet")
}

func (q *workspaceCreateQueries) ListActivity(context.Context, ports.ListActivityRequest) (readmodel.ActivityReadModel, error) {
	q.activityCalls++
	return readmodel.ActivityReadModel{}, nil
}

func (c *workspaceCreateCommands) RenameWorkspace(_ context.Context, request ports.RenameWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
	c.saved = request
	c.saveCalls++
	return readmodel.WorkspaceReadModel{ID: "+", Slug: request.Slug, State: readmodel.WorkspaceDraft}, nil
}

func (c *workspaceCreateCommands) DeleteWorkspace(context.Context, ports.DeleteWorkspaceRequest) error {
	c.deleteCalls++
	return nil
}

func TestCockpit_ConventionalFirstPromotionPreservesEndpointAndStartsPersistedLifetime(t *testing.T) {
	bootstrap := readmodel.NewConventionalFirstWorkspace("http://127.0.0.1:7926/c/default", nil)
	model := readmodel.CockpitReadModel{
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "default", Slug: "default", Kind: readmodel.WorkspaceTabBootstrap, Selected: true},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
		SelectedWorkspaceID: "default", SelectedWorkspace: bootstrap, ActivePage: readmodel.CockpitWorkspacePage,
	}
	queries := &workspaceCreateQueries{}
	root := NewCockpitWithContext(model, context.Background(), queries, &workspaceCreateCommands{})
	if got := activeWorkspaceMountKey(root.activeModel()); got != "workspace-page:default:bootstrap" {
		t.Fatalf("bootstrap mount key = %q", got)
	}
	h, err := testkit.NewHarnessAt(root, 100, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.Open()
	if queries.activityCalls != 0 {
		t.Fatalf("bootstrap activity calls = %d, want 0", queries.activityCalls)
	}

	committed := readmodel.WorkspaceReadModel{
		ID: "default", Slug: "default", State: readmodel.WorkspaceExisting,
		WorkspaceURL: bootstrap.WorkspaceURL,
		Routes:       []readmodel.RouteReadModel{{ID: "coding", ModelName: "coding", Enabled: true}},
	}
	page := root.currentWorkspacePage()
	page.RoutesSection.TargetConfigs.Callbacks.OnCreated(ports.SaveTargetResult{
		Route: committed.Routes[0], Workspace: committed,
	})
	h.Frame()

	active := root.activeModel()
	if !active.SelectedWorkspace.IsPersisted() || active.SelectedWorkspace.WorkspaceURL != bootstrap.WorkspaceURL {
		t.Fatalf("promoted workspace = %#v", active.SelectedWorkspace)
	}
	if got := activeWorkspaceMountKey(active); got != "workspace-page:default" {
		t.Fatalf("persisted mount key = %q", got)
	}
	if len(active.Tabs) != 3 || active.Tabs[0].Kind != readmodel.WorkspaceTabExisting || active.Tabs[1].Kind != readmodel.WorkspaceTabDraft || active.Tabs[2].Kind != readmodel.WorkspaceTabHelp {
		t.Fatalf("promoted tabs = %#v, want [default, +, ?]", active.Tabs)
	}
	if queries.loadCockpitCalls != 0 || queries.loadWorkspaceCalls != 0 {
		t.Fatalf("bootstrap promotion reloaded daemon: cockpit=%d workspace=%d", queries.loadCockpitCalls, queries.loadWorkspaceCalls)
	}
	if queries.activityCalls == 0 {
		t.Fatal("persisted remount did not start Activity lifecycle")
	}
	frame := h.FrameTrimmed()
	for _, want := range []string{"[› default]", "[+]", "[?]", "/c/default", "activity"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("promoted frame missing %q:\n%s", want, frame)
		}
	}
}

func TestRemoveLastWorkspaceSynthesizesConventionalFirstProjection(t *testing.T) {
	model := readmodel.CockpitReadModel{
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "dev", Slug: "dev", Kind: readmodel.WorkspaceTabExisting, Selected: true},
			{ID: "+", Kind: readmodel.WorkspaceTabDraft},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
		SelectedWorkspaceID: "dev",
		SelectedWorkspace: readmodel.WorkspaceReadModel{
			ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting,
			WorkspaceURL:    "http://127.0.0.1:9000/c/dev",
			ProviderOptions: []readmodel.ProviderOptionReadModel{{ProviderSpec: "openai", DisplayName: "OpenAI"}},
		},
		ActivePage: readmodel.CockpitWorkspacePage,
	}

	got := removeWorkspaceFromModel(model, "dev")
	if !got.SelectedWorkspace.IsBootstrap() || got.SelectedWorkspace.ID != "default" || got.SelectedWorkspace.WorkspaceURL != "http://127.0.0.1:9000/c/default" {
		t.Fatalf("last-delete projection = %#v", got.SelectedWorkspace)
	}
	if len(got.SelectedWorkspace.ProviderOptions) != 1 || got.SelectedWorkspace.ProviderOptions[0].ProviderSpec != "openai" {
		t.Fatalf("last-delete provider options = %#v", got.SelectedWorkspace.ProviderOptions)
	}
	if len(got.Tabs) != 2 || got.Tabs[0].Kind != readmodel.WorkspaceTabBootstrap || got.Tabs[1].Kind != readmodel.WorkspaceTabHelp {
		t.Fatalf("last-delete tabs = %#v, want [default, ?]", got.Tabs)
	}
}

func TestRemoveDeletedWorkspacePreservesFreshBootstrapProjection(t *testing.T) {
	bootstrap := readmodel.NewConventionalFirstWorkspace(
		"http://127.0.0.1:9000/c/default",
		[]readmodel.ProviderOptionReadModel{{ProviderSpec: "openai", DisplayName: "OpenAI"}},
	)
	model := readmodel.CockpitReadModel{
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "default", Slug: "default", Kind: readmodel.WorkspaceTabBootstrap, Selected: true},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
		SelectedWorkspaceID: "default", SelectedWorkspace: bootstrap, ActivePage: readmodel.CockpitWorkspacePage,
	}

	got := removeWorkspaceFromModel(model, "deleted-workspace")
	if !got.SelectedWorkspace.IsBootstrap() || got.SelectedWorkspace.WorkspaceURL != bootstrap.WorkspaceURL {
		t.Fatalf("fresh bootstrap reconciliation = %#v", got.SelectedWorkspace)
	}
	if len(got.SelectedWorkspace.ProviderOptions) != 1 || got.SelectedWorkspace.ProviderOptions[0].ProviderSpec != "openai" {
		t.Fatalf("fresh bootstrap provider options = %#v", got.SelectedWorkspace.ProviderOptions)
	}
}

func TestReplaceWorkspaceIdentityRekeysRegistryAndTab(t *testing.T) {
	old := readmodel.WorkspaceReadModel{ID: "foo", Slug: "foo", State: readmodel.WorkspaceExisting}
	model := readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "foo", Slug: "foo", Kind: readmodel.WorkspaceTabExisting, Selected: true}, {ID: "+", Kind: readmodel.WorkspaceTabDraft}},
		SelectedWorkspaceID: "foo", SelectedWorkspace: old, Workspaces: map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel{"foo": old},
	}
	newWorkspace := readmodel.WorkspaceReadModel{ID: "bar", Slug: "bar", State: readmodel.WorkspaceExisting}
	got := replaceWorkspaceIdentity(model, "foo", newWorkspace)
	if _, ok := got.Workspaces["foo"]; ok {
		t.Fatal("old workspace identity remains in registry")
	}
	if got.Workspaces["bar"].Slug != "bar" || got.Tabs[0].ID != "bar" || got.Tabs[0].Slug != "bar" {
		t.Fatalf("rename identity was not atomically rekeyed: %#v", got)
	}
}

func TestRemoveWorkspaceClearsRegistry(t *testing.T) {
	workspace := readmodel.WorkspaceReadModel{ID: "foo", Slug: "foo", State: readmodel.WorkspaceExisting}
	model := readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "foo", Slug: "foo", Kind: readmodel.WorkspaceTabExisting, Selected: true}, {ID: "+", Kind: readmodel.WorkspaceTabDraft}},
		SelectedWorkspaceID: "foo", SelectedWorkspace: workspace, Workspaces: map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel{"foo": workspace},
	}
	got := removeWorkspaceFromModel(model, "foo")
	if _, ok := got.Workspaces["foo"]; ok {
		t.Fatal("deleted workspace remains in registry")
	}
}

func TestCockpit_DraftWorkspaceNameEnterContinuesLocalOnboarding(t *testing.T) {
	model := readmodel.CockpitReadModel{
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "dev", Slug: "dev", Kind: readmodel.WorkspaceTabExisting},
			{ID: "+", Kind: readmodel.WorkspaceTabDraft, Selected: true},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
		// The durable snapshot still points at dev while ActiveTabIndex is derived
		// from the selected [+] tab. Save must use that active projection.
		SelectedWorkspaceID: "dev",
		SelectedWorkspace: readmodel.WorkspaceReadModel{
			ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting,
			Routes: []readmodel.RouteReadModel{{ID: "aws"}},
		},
		ActivePage: readmodel.CockpitWorkspacePage,
	}
	commands := &workspaceCreateCommands{}
	queries := &workspaceCreateQueries{}
	root := NewCockpitWithContext(model, context.Background(), queries, commands)
	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	for _, r := range "buildweek" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if commands.saveCalls != 0 {
		t.Fatalf("draft naming crossed workspace save port %d times", commands.saveCalls)
	}
	if queries.loadCockpitCalls != 0 || queries.loadWorkspaceCalls != 0 {
		t.Fatalf("draft promotion queried daemon: cockpit=%d workspace=%d", queries.loadCockpitCalls, queries.loadWorkspaceCalls)
	}
	active := root.activeModel()
	if active.SelectedWorkspace.ID != "+" || !active.SelectedWorkspace.IsDraft() {
		t.Fatalf("named draft was promoted before first target: %#v", active.SelectedWorkspace)
	}
	if len(active.SelectedWorkspace.Routes) != 0 {
		t.Fatalf("named draft inherited prior routes: %#v", active.SelectedWorkspace.Routes)
	}
	if warning := root.RefreshWarning.Get(); strings.TrimSpace(warning) != "" {
		t.Fatalf("draft promotion showed stale refresh warning: %q", warning)
	}
	frame := h.FrameTrimmed()
	if !strings.Contains(frame, "model routes") || !strings.Contains(frame, "discard") {
		t.Fatalf("named draft did not expose onboarding and local discard:\n%s", frame)
	}
	if strings.Contains(frame, "delete workspace") || commands.deleteCalls != 0 {
		t.Fatalf("named draft exposed daemon deletion: calls=%d\n%s", commands.deleteCalls, frame)
	}
	if got := strings.Count(frame, "> "); got != 1 {
		t.Fatalf("named draft frame has %d active markers, want one:\n%s", got, frame)
	}
	if strings.Contains(frame, "buildweek_") {
		t.Fatalf("submitted workspace name retained an edit caret:\n%s", frame)
	}
}

func TestCockpit_NamingDraftPreservesExistingWorkspaceRoutes(t *testing.T) {
	personalRoute := readmodel.RouteReadModel{ID: "personal-route", ModelName: "personal-route", Enabled: true}
	model := readmodel.CockpitReadModel{
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "personal", Slug: "personal", Kind: readmodel.WorkspaceTabExisting, Selected: true},
			{ID: "+", Kind: readmodel.WorkspaceTabDraft},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
		SelectedWorkspaceID: "personal",
		SelectedWorkspace: readmodel.WorkspaceReadModel{
			ID: "personal", Slug: "personal", State: readmodel.WorkspaceExisting,
			Routes: []readmodel.RouteReadModel{personalRoute},
		},
		ActivePage: readmodel.CockpitWorkspacePage,
	}
	commands := &workspaceCreateCommands{}
	root := NewCockpitWithContext(model, context.Background(), &workspaceCreateQueries{}, commands)
	h, err := testkit.NewHarnessAt(root, 100, 24)
	if err != nil {
		t.Fatalf("NewHarnessAt: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyTab})
	for _, r := range "demo" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if commands.saveCalls != 0 {
		t.Fatalf("draft naming crossed workspace save port %d times", commands.saveCalls)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift})

	frame := h.FrameTrimmed()
	if !strings.Contains(frame, "[› personal]") || !strings.Contains(frame, "personal-route") {
		t.Fatalf("existing workspace lost routes after named-draft round trip:\n%s", frame)
	}
}

func TestCockpit_WorkspaceFeatureCannotPublishRootRefreshWarning(t *testing.T) {
	model := readmodel.CockpitReadModel{
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "personal", Slug: "personal", Kind: readmodel.WorkspaceTabExisting, Selected: true},
			{ID: "+", Kind: readmodel.WorkspaceTabDraft},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
		SelectedWorkspaceID: "personal",
		SelectedWorkspace: readmodel.WorkspaceReadModel{
			ID: "personal", Slug: "personal", State: readmodel.WorkspaceExisting,
			WorkspaceURL: "http://127.0.0.1:7926/c/personal",
		},
		ActivePage: readmodel.CockpitWorkspacePage,
	}
	root := NewCockpitWithContext(model, context.Background(), &workspaceCreateQueries{}, &workspaceCreateCommands{})
	page := root.currentWorkspacePage()
	before := testkit.RenderMountedTrimmed(t, root, 100, 24)
	page.OverviewSection.WorkspaceWarning.Set("refresh unavailable")
	after := testkit.RenderMountedTrimmed(t, root, 100, 24)

	if got := root.RefreshWarning.Get(); got != "" {
		t.Fatalf("local workspace feedback escaped into root: %q", got)
	}
	if strings.Split(before, "\n")[0] != strings.Split(after, "\n")[0] {
		t.Fatalf("local feedback changed shell header:\nbefore: %q\nafter:  %q", strings.Split(before, "\n")[0], strings.Split(after, "\n")[0])
	}
}

func TestCockpit_WorkspaceAndRouteShareFeedbackRemainSourceLocal(t *testing.T) {
	route := readmodel.RouteReadModel{ID: "coding", ModelName: "coding", Enabled: true, Share: &readmodel.ShareReadModel{Hostname: "d-route.share.swobu.com", Never: true}}
	workspace := readmodel.WorkspaceReadModel{ID: "personal", Slug: "personal", State: readmodel.WorkspaceExisting, Share: &readmodel.ShareReadModel{Hostname: "d-workspace.share.swobu.com", Never: true}, Routes: []readmodel.RouteReadModel{route}}
	commands := &workspaceCreateCommands{revealShare: func(ref string) (shares.Result, error) {
		if ref == "personal/coding" {
			return shares.Result{}, errors.New("route share unavailable")
		}
		return shares.Result{}, errors.New("workspace share unavailable")
	}}
	root := NewCockpitWithContext(readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "personal", Slug: "personal", Kind: readmodel.WorkspaceTabExisting, Selected: true}},
		SelectedWorkspaceID: "personal", SelectedWorkspace: workspace, ActivePage: readmodel.CockpitWorkspacePage,
	}, context.Background(), &workspaceCreateQueries{}, commands)
	page := root.currentWorkspacePage()
	page.RoutesSection.State.ExpandedRoute.Set(route.ID)
	headerBefore := strings.Split(testkit.RenderMountedTrimmed(t, root, 100, 24), "\n")[0]

	workspaceShare := overviewsection.WorkspaceShareRowComponent(page.OverviewSection).(*ui.Select)
	ui.SelectHeaderComponent(workspaceShare).Activate()
	workspaceFrame := testkit.RenderMountedTrimmed(t, root, 100, 24)
	if !strings.Contains(workspaceFrame, "workspace share unavailable") || strings.Contains(workspaceFrame, "route share unavailable") {
		t.Fatalf("workspace Share feedback crossed sources:\n%s", workspaceFrame)
	}

	routeShare := routessection.ShareRowComponent(page.RoutesSection, route).(*ui.Select)
	ui.SelectHeaderComponent(routeShare).Activate()
	routeFrame := testkit.RenderMountedTrimmed(t, root, 100, 24)
	if !strings.Contains(routeFrame, "workspace share unavailable") || !strings.Contains(routeFrame, "route share unavailable") {
		t.Fatalf("route Share feedback replaced workspace feedback:\n%s", routeFrame)
	}
	if got := root.RefreshWarning.Get(); got != "" {
		t.Fatalf("Share feedback escaped to root: %q", got)
	}
	if headerAfter := strings.Split(routeFrame, "\n")[0]; headerAfter != headerBefore {
		t.Fatalf("Share feedback changed shell header:\nbefore: %q\nafter:  %q", headerBefore, headerAfter)
	}
}

func TestCockpit_TabSwitchDoesNotCarryLocalWorkspaceFeedback(t *testing.T) {
	workspaceA := readmodel.WorkspaceReadModel{ID: "a", Slug: "a", State: readmodel.WorkspaceExisting}
	workspaceB := readmodel.WorkspaceReadModel{ID: "b", Slug: "b", State: readmodel.WorkspaceExisting}
	root := NewCockpit(readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "a", Slug: "a", Kind: readmodel.WorkspaceTabExisting, Selected: true}, {ID: "b", Slug: "b", Kind: readmodel.WorkspaceTabExisting}, {ID: "?", Kind: readmodel.WorkspaceTabHelp}},
		SelectedWorkspaceID: "a", SelectedWorkspace: workspaceA, Workspaces: map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel{"a": workspaceA, "b": workspaceB}, ActivePage: readmodel.CockpitWorkspacePage,
	})
	root.currentWorkspacePage().OverviewSection.WorkspaceWarning.Set("refresh unavailable")
	root.activateTab(1)
	if got := root.currentWorkspacePage().OverviewSection.WorkspaceWarning.Get(); got != "" {
		t.Fatalf("workspace A feedback leaked into workspace B: %q", got)
	}
	if root.RefreshWarning.Get() != "" {
		t.Fatalf("local feedback leaked into root: %q", root.RefreshWarning.Get())
	}
}

func TestCockpit_NamedDraftProviderSelectionKeepsInlineTargetConfig(t *testing.T) {
	route := readmodel.RouteReadModel{ID: "kimi-k3", ModelName: "kimi-k3", Enabled: true}
	model := readmodel.CockpitReadModel{
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "+", Slug: "kimi-wedge", Kind: readmodel.WorkspaceTabDraft, Selected: true},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
		SelectedWorkspaceID: "+",
		SelectedWorkspace: readmodel.WorkspaceReadModel{
			ID: "+", Slug: "kimi-wedge", State: readmodel.WorkspaceDraft,
			Routes:          []readmodel.RouteReadModel{route},
			ProviderOptions: []readmodel.ProviderOptionReadModel{{ProviderSpec: "kimi", DisplayName: "Kimi"}},
		},
		ActivePage: readmodel.CockpitWorkspacePage,
	}
	root := NewCockpitWithContext(model, context.Background(), &workspaceCreateQueries{}, &workspaceCreateCommands{})
	page := root.currentWorkspacePage()
	page.RoutesSection.State.ExpandedRoute.Set(route.ID)
	page.RoutesSection.AddTarget(route)
	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	for range 24 {
		if frame := h.FrameTrimmed(); strings.Contains(frame, "> Kimi") {
			break
		}
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	}
	if frame := h.FrameTrimmed(); !strings.Contains(frame, "> Kimi") {
		t.Fatalf("named-draft provider picker did not select Kimi:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	frame := h.FrameTrimmed()
	if !strings.Contains(frame, "new target · Kimi") || !strings.Contains(frame, "credential") {
		t.Fatalf("named-draft provider selection closed the inline target config:\n%s", frame)
	}
	if got := page.RoutesSection.State.AddTargetRoute.Get(); got != route.ID {
		t.Fatalf("add target route = %q, want %q", got, route.ID)
	}
}

func TestCockpit_DiscardStartsFreshDraftInteractionLifetime(t *testing.T) {
	model := readmodel.CockpitReadModel{
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "+", Kind: readmodel.WorkspaceTabDraft, Selected: true},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
		SelectedWorkspaceID: "+",
		SelectedWorkspace: readmodel.WorkspaceReadModel{
			ID: "+", State: readmodel.WorkspaceDraft,
			ProviderOptions: []readmodel.ProviderOptionReadModel{{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"}},
		},
		ActivePage: readmodel.CockpitWorkspacePage,
	}
	root := NewCockpitWithContext(model, context.Background(), &workspaceCreateQueries{}, &workspaceCreateCommands{})
	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	typeName := func(name string) {
		for _, r := range name {
			h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
		}
	}
	typeName("dev")
	if frame := h.FrameTrimmed(); !strings.Contains(frame, "http://127.0.0.1:7926/c/dev") {
		t.Fatalf("live endpoint preview does not share the name draft:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	// The continuation selects add-route. Moving twice up reaches local discard:
	// add-route -> routes disclosure -> discard.
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	if frame := h.FrameTrimmed(); !strings.Contains(frame, "> discard") {
		t.Fatalf("discard was not selected through mounted traversal:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if frame := h.FrameTrimmed(); !strings.Contains(frame, "discard dev?") || !strings.Contains(frame, "confirm ↵") {
		t.Fatalf("discard did not enter confirmation state:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if got := root.activeModel().SelectedWorkspace.ProviderOptions; len(got) != 1 || got[0].ProviderSpec != "openai" {
		t.Fatalf("discarded draft provider options = %#v, want preserved OpenAI option", got)
	}
	frame := h.FrameTrimmed()
	if !strings.Contains(frame, "> name") || !strings.Contains(frame, "enter a workspace name") {
		t.Fatalf("discard did not begin a fresh unnamed editor lifetime:\n%s", frame)
	}
	if strings.Contains(frame, "model routes") || strings.Contains(frame, "discard") {
		t.Fatalf("discard retained named-draft onboarding rows:\n%s", frame)
	}

	typeName("dev")
	if frame := h.FrameTrimmed(); !strings.Contains(frame, "http://127.0.0.1:7926/c/dev") {
		t.Fatalf("fresh draft endpoint preview did not follow live name:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame = h.FrameTrimmed()
	if !strings.Contains(frame, "model routes") || !strings.Contains(frame, "discard") || strings.Contains(frame, "dev_") {
		t.Fatalf("continue failed after discard:\n%s", frame)
	}

	// Continue through the same mounted journey that exposed the empty picker.
	for range 16 {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
		addRoute := root.currentWorkspacePage().RoutesSection.AddRouteRow
		if addRoute != nil && addRoute.Ref().El() != nil && h.App().Focused() == addRoute.Ref().El() {
			break
		}
	}
	frame = h.FrameTrimmed()
	addRoute := root.currentWorkspacePage().RoutesSection.AddRouteRow
	if addRoute == nil || addRoute.Ref().El() == nil || h.App().Focused() != addRoute.Ref().El() {
		t.Fatalf("add model route was not selected through mounted traversal:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // add model route
	if frame = h.FrameTrimmed(); !strings.Contains(frame, "draft") || !strings.Contains(frame, "name") {
		t.Fatalf("add model route did not open the draft route:\n%s", frame)
	}
	for range 16 {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
		frame = h.FrameTrimmed()
		if strings.Contains(frame, "> name") && strings.Contains(frame, "draft") {
			break
		}
	}
	if !strings.Contains(frame, "> name") || !strings.Contains(frame, "draft") {
		t.Fatalf("draft route name was not selected through mounted traversal:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // enter draft route name editor
	typeName("gpt5.6")
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // create draft route
	for range 8 {
		frame = h.FrameTrimmed()
		if strings.Contains(frame, "> add target") {
			break
		}
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	}
	if frame = h.FrameTrimmed(); !strings.Contains(frame, "> add target") {
		t.Fatalf("add target was not reachable after draft recreation:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame = h.FrameTrimmed()
	if !strings.Contains(frame, "OpenAI") || !strings.Contains(frame, "1 of 1 shown") || strings.Contains(frame, "(no matches)") {
		t.Fatalf("provider picker lost ambient options after discard:\n%s", frame)
	}
}

func TestCockpit_SameIDRefreshPreservesMountedRoutesAndDeliversAddRouteIntent(t *testing.T) {
	workspace := readmodel.WorkspaceReadModel{
		ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting,
		Routes: []readmodel.RouteReadModel{{ID: "chat", ModelName: "chat", Enabled: true}},
	}
	root := NewCockpit(readmodel.CockpitReadModel{
		Tabs:                []readmodel.WorkspaceTabReadModel{{ID: "dev", Slug: "dev", Kind: readmodel.WorkspaceTabExisting, Selected: true}},
		SelectedWorkspaceID: "dev", SelectedWorkspace: workspace,
		Workspaces: map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel{"dev": workspace},
		ActivePage: readmodel.CockpitWorkspacePage,
	})
	h, err := testkit.NewHarnessAt(root, 100, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.Open()

	mountedPage := root.currentWorkspacePage()
	mountedRoutes := mountedPage.RoutesSection
	mountedRoutes.State.ExpandedRoute.Set("chat")
	committed := workspace
	committed.Routes = append([]readmodel.RouteReadModel(nil), workspace.Routes...)
	committed.Routes[0].ModelName = "chat-updated"

	// Drive the production save callback: it requests the one-shot focus intent
	// before Cockpit rebuilds fresh PageView props under the same mount key.
	mountedPage.OverviewSection.OnWorkspaceSaved(committed)
	if freshRoutes := root.currentWorkspacePage().RoutesSection; freshRoutes == mountedRoutes {
		t.Fatal("same-ID refresh did not produce a fresh routes prop snapshot")
	}
	h.Frame()

	if got := mountedRoutes.State.ExpandedRoute.Get(); got != "chat" {
		t.Fatalf("mounted route state after refresh = %q, want preserved chat", got)
	}
	addRoute := mountedRoutes.AddRouteRow
	if addRoute == nil || addRoute.Ref().El() == nil || h.App().Focused() != addRoute.Ref().El() {
		t.Fatalf("same-ID refresh lost add-route focus intent:\n%s", h.FrameTrimmed())
	}
}
