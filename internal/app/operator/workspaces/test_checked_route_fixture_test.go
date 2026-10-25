package workspaces

import (
	"context"
	"testing"
)

// addRouteFixture exercises the surviving reviewed route-creation contract.
// It does not create synthetic connection registry entries.
func addRouteFixture(t *testing.T, service Service, workspace, name string, target TargetDraft) Workspace {
	t.Helper()
	before, err := service.GetWorkspace(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := service.PutModelRouteChecked(t.Context(), ReplaceRoute{
		Workspace: workspace, Route: name,
		Spec: RouteSpec{Tiers: []TierSpec{{Targets: []TargetDraft{target}}}},
	}, before.ManagementETag, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return committed
}
