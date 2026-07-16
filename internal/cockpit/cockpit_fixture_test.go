package cockpit

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestFixture_DefaultWorkspace(t *testing.T) {
	assertCockpitFixtureWidths(t, "default_workspace", 20, []int{80, 100, 120}, func() *Cockpit {
		return newFixtureCockpit(DefaultFixtureReadModel(), nil)
	})
}

func TestFixture_CustomDaemonHeader(t *testing.T) {
	assertCockpitFixtureWidths(t, "custom_daemon_header", 20, []int{80, 100, 120}, func() *Cockpit {
		model := DefaultFixtureReadModel()
		model.HeaderRight = "http://pi:7926"
		return newFixtureCockpit(model, nil)
	})
}

func TestFixture_LastWorkspaceDeletedDraftActive(t *testing.T) {
	assertCockpitFixtureWidths(t, "last_workspace_deleted_draft_active", 20, []int{80, 100, 120}, func() *Cockpit {
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
		return newFixtureCockpit(removeWorkspaceFromModel(model, "dev"), nil)
	})
}

func TestFixture_StaticStates(t *testing.T) {
	tests := []struct {
		name   string
		height int
		view   func() *Cockpit
	}{
		{name: "route_focused", height: 22, view: routeFocusedFixtureCockpit},
		{name: "route_expanded", height: 34, view: routeExpandedFixtureCockpit},
		{name: "exceptional_routes", height: 24, view: exceptionalRoutesFixtureCockpit},
		{name: "activity_latest", height: 22, view: func() *Cockpit { return activityLatestFixtureCockpit(false, false) }},
		{name: "activity_error", height: 22, view: func() *Cockpit { return activityLatestFixtureCockpit(true, false) }},
		{name: "activity_expanded", height: 27, view: func() *Cockpit { return activityLatestFixtureCockpit(false, true) }},
		{name: "draft_workspace", height: 20, view: draftWorkspaceFixtureCockpit},
		{name: "help", height: 16, view: func() *Cockpit { return helpFixtureCockpit(readmodel.DiagnosticsReady) }},
		{name: "help_copied", height: 16, view: func() *Cockpit { return helpFixtureCockpit(readmodel.DiagnosticsCopied) }},
		{name: "delete_confirm", height: 22, view: deleteConfirmFixtureCockpit},
		{name: "all_collapsed", height: 16, view: collapsedFixtureCockpit},
		{name: "first_target_ready", height: 30, view: firstTargetReadyFixtureCockpit},
		{name: "first_target_created", height: 30, view: firstTargetCreatedFixtureCockpit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertCockpitFixtureWidths(t, tt.name, tt.height, []int{80, 100, 120}, tt.view)
		})
	}
}

func assertCockpitFixtureWidths(t *testing.T, name string, height int, widths []int, view func() *Cockpit) {
	t.Helper()
	for _, width := range widths {
		width := width
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			rendered := testkit.RenderMountedTrimmed(t, view(), width, height)
			fixtureName := fmt.Sprintf("%s_%d", name, width)
			testkit.AssertVisual(fixtureName).
				Fixture(fmt.Sprintf("testdata/cockpit_fixture__testfixture_%s/fixture/%s.txt", strings.ReplaceAll(name, "_", ""), fixtureName)).
				Normalize(trimRightLines).
				Viewport(width, height).
				Now(t, rendered)
		})
	}
}

func trimRightLines(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

func newFixtureCockpit(model readmodel.CockpitReadModel, configure func(*Cockpit)) *Cockpit {
	cockpit := NewCockpit(model)
	if configure != nil {
		configure(cockpit)
	}
	return cockpit
}

func routeFocusedFixtureCockpit() *Cockpit {
	return newFixtureCockpit(DefaultFixtureReadModel(), nil)
}

func routeExpandedFixtureCockpit() *Cockpit {
	return newFixtureCockpit(DefaultFixtureReadModel(), func(c *Cockpit) {
		c.currentWorkspacePage().RoutesSection.OpenRoute(c.currentWorkspacePage().RoutesSection.State.Routes[0])
	})
}

func exceptionalRoutesFixtureCockpit() *Cockpit {
	model := DefaultFixtureReadModel()
	model.SelectedWorkspace.Routes = []readmodel.RouteReadModel{
		model.SelectedWorkspace.Routes[0],
		{
			ID:        "free",
			ModelName: "free",
			State:     readmodel.RouteDegraded,
			Enabled:   true,
			Targets: []readmodel.TargetReadModel{
				{ID: "free-1"}, {ID: "free-2"}, {ID: "free-3"},
			},
			Diagnostics: []readmodel.RouteDiagnosticReadModel{{
				Kind:       readmodel.RouteDiagnosticRateLimited,
				StatusCode: 429,
				Count:      9,
			}},
		},
		{
			ID:        "local",
			ModelName: "local",
			State:     readmodel.RouteBlocked,
			Enabled:   true,
			Targets:   []readmodel.TargetReadModel{{ID: "local-1"}},
			Diagnostics: []readmodel.RouteDiagnosticReadModel{{
				Kind: readmodel.RouteDiagnosticUnreachable,
			}},
		},
		{
			ID:        "route-new",
			ModelName: "route-new",
			State:     readmodel.RouteNormal,
			Enabled:   true,
		},
	}
	return newFixtureCockpit(model, nil)
}

func activityLatestFixtureCockpit(errorRow bool, expanded bool) *Cockpit {
	model := DefaultFixtureReadModel()
	row := readmodel.ActivityRowReadModel{
		ID:          "req-1",
		ObservedAt:  "14:32:01",
		ClientLabel: "codex",
		RouteID:     "gpt",
		RouteLabel:  "gpt",
		Status:      readmodel.ActivitySucceeded,
		HTTPStatus:  200,
		Duration:    145 * time.Millisecond,
		Attempts: []readmodel.ActivityAttemptReadModel{{
			Label:  "gpt-4.1",
			Rank:   1,
			Result: readmodel.ActivityAttemptSucceeded,
		}},
		TokensIn:  1200,
		TokensOut: 450,
	}
	if errorRow {
		row.Status = readmodel.ActivityFailed
		row.HTTPStatus = 401
		row.Duration = 23 * time.Millisecond
		row.Error = true
	}
	model.SelectedWorkspace.Activity = readmodel.ActivityReadModel{Latest: &row}
	return newFixtureCockpit(model, func(c *Cockpit) {})
}

func draftWorkspaceFixtureCockpit() *Cockpit {
	model := DefaultFixtureReadModel()
	for i := range model.Tabs {
		model.Tabs[i].Selected = model.Tabs[i].Kind == readmodel.WorkspaceTabDraft
	}
	model.SelectedWorkspaceID = "+"
	model.SelectedWorkspace = readmodel.WorkspaceReadModel{
		ID:    "+",
		State: readmodel.WorkspaceDraft,
	}
	return newFixtureCockpit(model, nil)
}

func helpFixtureCockpit(status readmodel.DiagnosticsStatus) *Cockpit {
	model := DefaultFixtureReadModel()
	for i := range model.Tabs {
		model.Tabs[i].Selected = model.Tabs[i].Kind == readmodel.WorkspaceTabHelp
	}
	model.SelectedWorkspaceID = "?"
	model.ActivePage = readmodel.CockpitHelpPage
	model.Help.DiagnosticsStatus = status
	return newFixtureCockpit(model, nil)
}

func deleteConfirmFixtureCockpit() *Cockpit {
	return newFixtureCockpit(DefaultFixtureReadModel(), func(c *Cockpit) {
		c.currentWorkspacePage().OverviewSection.OpenDeleteConfirmation("dev")
	})
}

func collapsedFixtureCockpit() *Cockpit {
	return newFixtureCockpit(DefaultFixtureReadModel(), func(c *Cockpit) {
		c.currentWorkspacePage().RoutesSection.Expanded.Set(false)
	})
}

func firstTargetCreatedFixtureCockpit() *Cockpit {
	model := DefaultFixtureReadModel()
	// Simulate: workspace "lab" with route "gpt" and one new target
	// Route "gpt" has 1 target in step 1 (typical first-target outcome).
	model.Tabs = []readmodel.WorkspaceTabReadModel{
		{ID: "lab", Slug: "lab", Kind: readmodel.WorkspaceTabExisting, Selected: true},
		{ID: "dev", Slug: "dev", Kind: readmodel.WorkspaceTabExisting},
		{ID: "+", Kind: readmodel.WorkspaceTabDraft},
		{ID: "?", Kind: readmodel.WorkspaceTabHelp},
	}
	model.SelectedWorkspaceID = "lab"
	model.SelectedWorkspace = readmodel.WorkspaceReadModel{
		ID:            "lab",
		Slug:          "lab",
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/lab",
		Routes: []readmodel.RouteReadModel{
			{
				ID:        "gpt",
				ModelName: "gpt",
				State:     readmodel.RouteNormal,
				Default:   true,
				Enabled:   true,
				Targets: []readmodel.TargetReadModel{
					{
						ID:            "t-new",
						Name:          "openai/gpt-4.1",
						Provider:      "openai",
						Model:         "gpt-4.1",
						BaseURL:       "https://api.openai.com/v1",
						CredentialRef: "env:OPENAI_API_KEY",
						Rank:          1,
						Weight:        1,
					},
				},
			},
		},
	}
	return newFixtureCockpit(model, func(c *Cockpit) {
		// Expand the route so the target is visible.
		c.currentWorkspacePage().RoutesSection.OpenRoute(c.currentWorkspacePage().RoutesSection.State.Routes[0])
	})
}

func firstTargetReadyFixtureCockpit() *Cockpit {
	model := DefaultFixtureReadModel()
	model.Tabs = []readmodel.WorkspaceTabReadModel{
		{ID: "lab", Slug: "lab", Kind: readmodel.WorkspaceTabExisting, Selected: true},
		{ID: "dev", Slug: "dev", Kind: readmodel.WorkspaceTabExisting},
		{ID: "+", Kind: readmodel.WorkspaceTabDraft},
		{ID: "?", Kind: readmodel.WorkspaceTabHelp},
	}
	model.SelectedWorkspaceID = "lab"
	model.SelectedWorkspace = readmodel.WorkspaceReadModel{
		ID:            "lab",
		Slug:          "lab",
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/lab",
		Routes: []readmodel.RouteReadModel{
			{
				ID:        "gpt",
				ModelName: "gpt",
				State:     readmodel.RouteNormal,
				Default:   true,
				Enabled:   true,
			},
		},
	}
	return newFixtureCockpit(model, func(c *Cockpit) {
		c.currentWorkspacePage().RoutesSection.TargetSetupQueries = readyTargetSetupQueries{}
		route := c.currentWorkspacePage().RoutesSection.State.Routes[0]
		c.currentWorkspacePage().RoutesSection.OpenRoute(route)
		c.currentWorkspacePage().RoutesSection.AddTarget(route)
		wf := c.currentWorkspacePage().RoutesSection.TargetAddWorkflows[route.ID]
		if wf == nil {
			panic("expected open target-add workflow")
		}
		wf.SelectProvider("openai")
		wf.SetCatalogResult(readmodel.ModelCatalogReadModel{
			Deployments: []readmodel.ModelDeploymentReadModel{
				{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
			},
		})
		wf.SelectModel(readmodel.ModelDeploymentReadModel{
			ID:                      "gpt-4.1",
			Name:                    "GPT-4.1",
			ModelName:               "gpt-4.1",
			DefaultProviderProtocol: "chat_completions",
		})
	})
}

type readyTargetSetupQueries struct{}

func (readyTargetSetupQueries) ListTargetProviders(context.Context) ([]readmodel.ProviderOptionReadModel, error) {
	return nil, nil
}

func (readyTargetSetupQueries) ResolveProviderSetup(context.Context, ports.ResolveProviderSetupRequest) (readmodel.ProviderSetupReadModel, error) {
	return readmodel.ProviderSetupReadModel{
		ProviderSpec:       "openai",
		DisplayName:        "OpenAI",
		CredentialLabel:    "env:OPENAI_API_KEY",
		CredentialRef:      "env:OPENAI_API_KEY",
		CredentialRequired: true,
		ReadyForCatalog:    true,
		DefaultBaseURL:     "https://api.openai.com/v1",
	}, nil
}

func (readyTargetSetupQueries) ProbeProviderModels(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
	return readmodel.ModelCatalogReadModel{}, nil
}
