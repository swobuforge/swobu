package cockpit

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
			root := view().Render(nil)
			rendered := testkit.RenderTrimmed(root, width, height)
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
		c.WorkspacePage.RoutesSection.OpenRoute(c.WorkspacePage.RoutesSection.State.Routes[0])
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
			PlanKind:  readmodel.RoutePlanWeighted,
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
			PlanKind:  readmodel.RoutePlanSingle,
			Enabled:   true,
			Targets:   []readmodel.TargetReadModel{{ID: "local-1"}},
			Diagnostics: []readmodel.RouteDiagnosticReadModel{{
				Kind: readmodel.RouteDiagnosticUnreachable,
			}},
		},
		{
			ID:        "route-new",
			ModelName: "route-new",
			State:     readmodel.RouteIncomplete,
			Enabled:   true,
		},
	}
	return newFixtureCockpit(model, nil)
}

func activityLatestFixtureCockpit(errorRow bool, expanded bool) *Cockpit {
	model := DefaultFixtureReadModel()
	row := readmodel.ActivityRowReadModel{
		ID:           "req-1",
		ObservedAt:   "14:32:01",
		ClientLabel:  "codex",
		RouteID:      "gpt",
		RouteLabel:   "gpt",
		Status:       readmodel.ActivitySucceeded,
		HTTPStatus:   200,
		Duration:     145 * time.Millisecond,
		ResolvedName: "gpt",
		Model:        "gpt-4.1",
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
	return newFixtureCockpit(model, func(c *Cockpit) {
		if expanded {
			c.WorkspacePage.ActivitySection.OpenActivity.Set(row.ID)
		}
	})
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
		c.WorkspacePage.OverviewSection.OpenDeleteConfirmation("dev")
	})
}

func collapsedFixtureCockpit() *Cockpit {
	return newFixtureCockpit(DefaultFixtureReadModel(), func(c *Cockpit) {
		c.WorkspacePage.OverviewSection.SummaryOnly.Set(true)
		c.WorkspacePage.RoutesSection.Expanded.Set(false)
		c.WorkspacePage.ActivitySection.Expanded.Set(false)
	})
}
