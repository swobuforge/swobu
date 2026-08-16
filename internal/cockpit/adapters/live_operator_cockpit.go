package adapters

import (
	"context"
	"sort"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/platform/config"
)

// LoadCockpit projects daemon-owned workspace truth into the Cockpit shell.
// An empty authoritative list produces the conventional first-workspace
// projection locally; it does not query or create a workspace named default.
func (a *LiveOperatorAdapter) LoadCockpit(ctx context.Context) (readmodel.CockpitReadModel, error) {
	summaries, err := a.client.ListWorkspaces(ctx)
	if err != nil {
		return readmodel.CockpitReadModel{}, adapterFailure("load cockpit data", err)
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Slug < summaries[j].Slug })
	model := readmodel.CockpitReadModel{HeaderRight: a.headerRight(), ActivePage: readmodel.CockpitWorkspacePage, Help: a.helpReadModel(ctx, summaries)}
	if len(summaries) == 0 {
		workspace := conventionalFirstWorkspace(a.workspaceURL(readmodel.ConventionalFirstWorkspaceSlug))
		model.SelectedWorkspaceID = workspace.ID
		model.SelectedWorkspace = workspace
		model.Tabs = []readmodel.WorkspaceTabReadModel{
			{ID: workspace.ID, Slug: workspace.Slug, Kind: readmodel.WorkspaceTabBootstrap, Selected: true},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		}
		return model, nil
	}
	selected, err := a.client.GetWorkspace(ctx, summaries[0].Slug)
	if err != nil {
		return readmodel.CockpitReadModel{}, adapterFailure("load selected workspace", err)
	}
	workspace, err := a.workspaceFromView(ctx, selected)
	if err != nil {
		return readmodel.CockpitReadModel{}, err
	}
	model.SelectedWorkspaceID = workspace.ID
	model.SelectedWorkspace = workspace
	for _, summary := range summaries {
		id := readmodel.WorkspaceID(summary.Slug)
		model.Tabs = append(model.Tabs, readmodel.WorkspaceTabReadModel{ID: id, Slug: summary.Slug, Kind: readmodel.WorkspaceTabExisting, Selected: id == model.SelectedWorkspaceID})
	}
	model.Tabs = append(model.Tabs, readmodel.WorkspaceTabReadModel{ID: "+", Kind: readmodel.WorkspaceTabDraft}, readmodel.WorkspaceTabReadModel{ID: "?", Kind: readmodel.WorkspaceTabHelp})
	return model, nil
}

func (a *LiveOperatorAdapter) helpReadModel(ctx context.Context, summaries []workspaceapi.WorkspaceSummary) readmodel.HelpReadModel {
	version, _ := a.client.DaemonVersion(ctx)
	payload := readmodel.DiagnosticsPayload{Version: controlplane.SwobuVersion(), DaemonVersion: version, ConfigPath: config.DefaultConfigPath()}
	for _, summary := range summaries {
		payload.Workspaces = append(payload.Workspaces, readmodel.DiagnosticsWorkspacePayload{Name: summary.Slug, ActivityCount: a.activityCount(ctx, summary.Slug)})
	}
	return readmodel.HelpReadModel{Version: controlplane.SwobuVersion(), DaemonVersion: version, Diagnostics: payload}
}

func (a *LiveOperatorAdapter) activityCount(ctx context.Context, slug string) int {
	projection, err := a.client.Status(ctx, "workspace:"+slug)
	if err != nil {
		return 0
	}
	return len(projection.RecentTraffic)
}
