package cockpit

import (
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestFixture_DefaultWorkspace(t *testing.T) {
	root := NewCockpit(DefaultFixtureReadModel()).Render(nil)
	rendered := testkit.RenderTrimmed(root, 70, 20)
	testkit.AssertVisual("default_workspace").Normalize(trimRightLines).Viewport(70, 20).Now(t, rendered)
}

func TestFixture_StaticStates(t *testing.T) {
	tests := []struct {
		name   string
		height int
		model  readmodel.CockpitReadModel
	}{
		{name: "route_focused", height: 22, model: routeFocusedFixtureReadModel()},
		{name: "route_expanded", height: 34, model: routeExpandedFixtureReadModel()},
		{name: "exceptional_routes", height: 24, model: exceptionalRoutesFixtureReadModel()},
		{name: "activity_latest", height: 22, model: activityLatestFixtureReadModel(false, false)},
		{name: "activity_error", height: 22, model: activityLatestFixtureReadModel(true, false)},
		{name: "activity_expanded", height: 27, model: activityLatestFixtureReadModel(false, true)},
		{name: "draft_workspace", height: 20, model: draftWorkspaceFixtureReadModel()},
		{name: "help", height: 16, model: helpFixtureReadModel(readmodel.DiagnosticsReady)},
		{name: "help_copied", height: 16, model: helpFixtureReadModel(readmodel.DiagnosticsCopied)},
		{name: "delete_confirm", height: 22, model: deleteConfirmFixtureReadModel()},
		{name: "all_collapsed", height: 16, model: collapsedFixtureReadModel()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewCockpit(tt.model).Render(nil)
			rendered := testkit.RenderTrimmed(root, 70, tt.height)
			testkit.AssertVisual(tt.name).
				Fixture("testdata/cockpit_fixture__testfixture_staticstates/fixture/"+tt.name+".txt").
				Normalize(trimRightLines).
				Viewport(70, tt.height).
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

func routeFocusedFixtureReadModel() readmodel.CockpitReadModel {
	model := DefaultFixtureReadModel()
	model.SelectedWorkspace.View.FocusedRouteID = "gpt"
	return model
}

func routeExpandedFixtureReadModel() readmodel.CockpitReadModel {
	model := routeFocusedFixtureReadModel()
	model.SelectedWorkspace.View.ExpandedRouteID = "gpt"
	return model
}

func exceptionalRoutesFixtureReadModel() readmodel.CockpitReadModel {
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
	return model
}

func activityLatestFixtureReadModel(errorRow bool, expanded bool) readmodel.CockpitReadModel {
	model := DefaultFixtureReadModel()
	row := readmodel.ActivityRowReadModel{
		ID:           "req-1",
		At:           time.Date(2026, 6, 23, 14, 32, 1, 0, time.UTC),
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
	if expanded {
		model.SelectedWorkspace.View.FocusedActivityID = row.ID
		model.SelectedWorkspace.View.ExpandedActivityID = row.ID
	}
	return model
}

func draftWorkspaceFixtureReadModel() readmodel.CockpitReadModel {
	model := DefaultFixtureReadModel()
	for i := range model.Tabs {
		model.Tabs[i].Selected = model.Tabs[i].Kind == readmodel.WorkspaceTabDraft
	}
	model.SelectedWorkspaceID = "+"
	model.SelectedWorkspace = readmodel.WorkspaceReadModel{
		ID:    "+",
		State: readmodel.WorkspaceDraft,
		View: readmodel.WorkspaceViewState{
			WorkspaceExpanded: true,
			RoutesExpanded:    true,
			ActivityExpanded:  true,
		},
	}
	return model
}

func helpFixtureReadModel(status readmodel.DiagnosticsStatus) readmodel.CockpitReadModel {
	model := DefaultFixtureReadModel()
	for i := range model.Tabs {
		model.Tabs[i].Selected = model.Tabs[i].Kind == readmodel.WorkspaceTabHelp
	}
	model.SelectedWorkspaceID = "?"
	model.Surface = readmodel.CockpitHelpSurface
	model.Help.DiagnosticsStatus = status
	return model
}

func deleteConfirmFixtureReadModel() readmodel.CockpitReadModel {
	model := DefaultFixtureReadModel()
	model.SelectedWorkspace.View.DeleteWorkspaceConfirm = true
	model.SelectedWorkspace.View.WorkspaceConfirmationID = "dev"
	return model
}

func collapsedFixtureReadModel() readmodel.CockpitReadModel {
	model := DefaultFixtureReadModel()
	model.SelectedWorkspace.View.WorkspaceSummaryOnly = true
	model.SelectedWorkspace.View.RoutesExpanded = false
	model.SelectedWorkspace.View.ActivityExpanded = false
	return model
}
