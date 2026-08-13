package cockpit

import (
	"context"
	"errors"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	overviewsection "github.com/swobuforge/swobu/internal/cockpit/sections/workspace_overview"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

type workspaceCreateCommands struct {
	saved       ports.RenameWorkspaceRequest
	saveCalls   int
	deleteCalls int
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
}

func (q *workspaceCreateQueries) LoadCockpit(context.Context) (readmodel.CockpitReadModel, error) {
	q.loadCockpitCalls++
	return readmodel.CockpitReadModel{}, errors.New("draft workspace is not persisted yet")
}

func (q *workspaceCreateQueries) LoadWorkspace(context.Context, readmodel.WorkspaceID) (readmodel.WorkspaceReadModel, error) {
	q.loadWorkspaceCalls++
	return readmodel.WorkspaceReadModel{}, errors.New("draft workspace is not persisted yet")
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
	if notice := root.RefreshNotice.Get(); strings.TrimSpace(notice.Message) != "" {
		t.Fatalf("draft promotion showed stale refresh notice: %#v", notice)
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
