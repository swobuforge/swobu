package readmodel

import "testing"

func TestRouteReadModel_RowValueCoversWireframeStates(t *testing.T) {
	tests := []struct {
		name  string
		route RouteReadModel
		want  string
	}{
		{
			name: "default balanced",
			route: RouteReadModel{
				ModelName: "gpt",
				State:     RouteNormal,
				Default:   true,
				Enabled:   true,
				Targets:   []TargetReadModel{{ID: "target-1"}, {ID: "target-2"}},
			},
			want: "default · 2 balanced targets",
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
			name: "degraded balanced",
			route: RouteReadModel{
				ModelName: "free",
				State:     RouteDegraded,
				Enabled:   true,
				Targets:   []TargetReadModel{{ID: "a"}, {ID: "b"}, {ID: "c"}},
				Diagnostics: []RouteDiagnosticReadModel{{
					Kind:       RouteDiagnosticRateLimited,
					StatusCode: 429,
					Count:      9,
				}},
			},
			want: "3 balanced targets · degraded 429 x9",
		},
		{
			name: "blocked single",
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
			name: "fallback steps",
			route: RouteReadModel{
				ModelName: "safe",
				State:     RouteNormal,
				Enabled:   true,
				Targets: []TargetReadModel{
					{ID: "a", Rank: 1},
					{ID: "b", Rank: 2},
					{ID: "c", Rank: 3},
				},
			},
			want: "3 fallback steps",
		},
		{
			name: "mixed steps",
			route: RouteReadModel{
				ModelName: "gpt",
				State:     RouteNormal,
				Enabled:   true,
				Targets: []TargetReadModel{
					{ID: "a", Rank: 1},
					{ID: "b", Rank: 1},
					{ID: "c", Rank: 2},
				},
			},
			want: "2 steps · 3 targets",
		},
		{
			name: "no targets",
			route: RouteReadModel{
				ModelName: "route-new",
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
	noTargets := visible
	noTargets.Targets = nil
	if noTargets.IsClientVisible() {
		t.Fatal("route with no targets should not be client-visible")
	}
	disabled := visible
	disabled.Enabled = false
	if disabled.IsClientVisible() {
		t.Fatal("disabled route should not be client-visible")
	}
}

func TestWorkspaceReadModel_IsDraft(t *testing.T) {
	draft := WorkspaceReadModel{ID: "+", Slug: "+", State: WorkspaceDraft}
	if !draft.IsDraft() {
		t.Fatal("workspace with draft state should be draft")
	}
	existing := WorkspaceReadModel{ID: "dev", Slug: "dev", State: WorkspaceExisting}
	if existing.IsDraft() {
		t.Fatal("existing workspace should not be draft")
	}
}

func TestActivityReadModel_LatestRow(t *testing.T) {
	row := ActivityRowReadModel{ID: "req-1", Status: ActivitySucceeded}
	activity := ActivityReadModel{
		Latest: &row,
	}
	latest, ok := activity.LatestRow()
	if !ok || latest.ID != "req-1" {
		t.Fatalf("latest = %v, want req-1", latest)
	}
}
