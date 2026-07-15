package readmodel

import (
	"testing"
	"time"
)

func TestRouteReadModel_RowValueCoversWireframeStates(t *testing.T) {
	tests := []struct {
		name  string
		route RouteReadModel
		want  string
	}{
		{
			name: "default",
			route: RouteReadModel{
				ModelName: "gpt",
				State:     RouteNormal,
				Default:   true,
				Enabled:   true,
				Targets:   []TargetReadModel{{ID: "target-1"}, {ID: "target-2"}},
			},
			want: "default · 2 targets",
		},
		{
			name: "single target",
			route: RouteReadModel{
				ModelName: "local",
				State:     RouteNormal,
				Enabled:   true,
				Targets:   []TargetReadModel{{ID: "target-1"}},
			},
			want: "1 target",
		},
		{
			name: "degraded",
			route: RouteReadModel{
				ModelName: "free",
				State:     RouteDegraded,
				PlanKind:  RoutePlanWeighted,
				Enabled:   true,
				Targets:   []TargetReadModel{{ID: "a"}, {ID: "b"}, {ID: "c"}},
				Diagnostics: []RouteDiagnosticReadModel{{
					Kind:       RouteDiagnosticRateLimited,
					StatusCode: 429,
					Count:      9,
				}},
			},
			want: "3 weighted · degraded 429 x9",
		},
		{
			name: "blocked",
			route: RouteReadModel{
				ModelName: "local",
				State:     RouteBlocked,
				Enabled:   true,
				Targets:   []TargetReadModel{{ID: "target-1"}},
				Diagnostics: []RouteDiagnosticReadModel{{
					Kind: RouteDiagnosticUnreachable,
				}},
			},
			want: "1 target · blocked unreachable",
		},
		{
			name: "incomplete",
			route: RouteReadModel{
				ModelName: "route-new",
				State:     RouteIncomplete,
				Enabled:   true,
			},
			want: "incomplete · no targets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.route.RowValue(); got != tt.want {
				t.Fatalf("RowValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRouteReadModel_IsClientVisible(t *testing.T) {
	visible := RouteReadModel{
		State:   RouteNormal,
		Enabled: true,
		Targets: []TargetReadModel{{ID: "target-1"}},
	}
	if !visible.IsClientVisible() {
		t.Fatal("complete enabled route should be client-visible")
	}
	incomplete := visible
	incomplete.State = RouteIncomplete
	if incomplete.IsClientVisible() {
		t.Fatal("incomplete route should not be client-visible")
	}
	disabled := visible
	disabled.Enabled = false
	if disabled.IsClientVisible() {
		t.Fatal("disabled route should not be client-visible")
	}
}

func TestWorkspaceReadModel_RepresentsDefaultAndDraft(t *testing.T) {
	defaultWorkspace := WorkspaceReadModel{
		ID:            "dev",
		Slug:          "dev",
		State:         WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/dev",
		RunCommands: []RunCommandReadModel{{
			ID:          "codex",
			ClientID:    "codex",
			Label:       "Codex",
			CommandName: "codex",
		}},
		Routes: []RouteReadModel{{
			ID:        "gpt",
			ModelName: "gpt",
			State:     RouteNormal,
			Enabled:   true,
			Targets:   []TargetReadModel{{ID: "target-1"}},
		}},
	}
	if defaultWorkspace.IsDraft() {
		t.Fatal("existing workspace should not be draft")
	}
	if !defaultWorkspace.HasRoutes() {
		t.Fatal("default workspace should represent route rows")
	}

	draftWorkspace := WorkspaceReadModel{State: WorkspaceDraft}
	if !draftWorkspace.IsDraft() {
		t.Fatal("draft workspace should report draft state")
	}
	if draftWorkspace.HasRoutes() {
		t.Fatal("draft workspace should represent no routes before creation")
	}
}

func TestRunCommandReadModel_Disclosure(t *testing.T) {
	command := RunCommandReadModel{
		CommandName:   "codex",
		TargetRouteID: "gpt",
		TargetLabel:   "gpt",
		Effect:        RunCommandExecutesClient,
	}
	if got, want := command.DisclosureValue(), "codex -> gpt"; got != want {
		t.Fatalf("DisclosureValue() = %q, want %q", got, want)
	}
	if got, want := command.EffectLabel(), "executes client"; got != want {
		t.Fatalf("EffectLabel() = %q, want %q", got, want)
	}
}

func TestActivityReadModel_LatestAndRowValue(t *testing.T) {
	row := ActivityRowReadModel{
		ID:          "req-1",
		ObservedAt:  "14:32:01",
		ClientLabel: "codex",
		RouteID:     "gpt",
		RouteLabel:  "gpt",
		Status:      ActivitySucceeded,
		HTTPStatus:  200,
		Duration:    145 * time.Millisecond,
	}
	activity := ActivityReadModel{Rows: []ActivityRowReadModel{row}}
	latest, ok := activity.LatestRow()
	if !ok {
		t.Fatal("LatestRow() did not derive row from Rows")
	}
	if latest.ID != row.ID {
		t.Fatalf("LatestRow().ID = %q, want %q", latest.ID, row.ID)
	}
	if got, want := row.RowValue(), "14:32:01  codex  gpt  200  145ms"; got != want {
		t.Fatalf("RowValue() = %q, want %q", got, want)
	}
	if (ActivityReadModel{}).IsEmpty() != true {
		t.Fatal("zero activity should be empty")
	}
}

func TestHelpReadModel_SupportOrientationCopy(t *testing.T) {
	model := HelpReadModel{
		Version:        "swobu 0.4.1",
		CockpitVersion: "cockpit v2",
		Commit:         "a1b2c3d",
		DocsURL:        "swobu.com/docs",
		CommunityURL:   "https://discord.gg/swobu",
		IssueURL:       "https://github.com/swobuforge/swobu/issues/new",
	}
	if got, want := model.VersionValue(), "swobu 0.4.1 · cockpit v2 · commit a1b2c3d"; got != want {
		t.Fatalf("VersionValue() = %q, want %q", got, want)
	}
	if got, want := model.DocsValue(), "swobu.com/docs"; got != want {
		t.Fatalf("DocsValue() = %q, want %q", got, want)
	}
	if got, want := model.CommunityValue(), "Discord"; got != want {
		t.Fatalf("CommunityValue() = %q, want %q", got, want)
	}
	if got, want := model.IssueValue(), "GitHub issue"; got != want {
		t.Fatalf("IssueValue() = %q, want %q", got, want)
	}
	if got, want := model.DiagnosticsValue(), "copy report context"; got != want {
		t.Fatalf("DiagnosticsValue() = %q, want %q", got, want)
	}

	model.DiagnosticsStatus = DiagnosticsCopied
	if got, want := model.DiagnosticsValue(), "copied · paste into issue/Discord"; got != want {
		t.Fatalf("DiagnosticsValue(copied) = %q, want %q", got, want)
	}
}
