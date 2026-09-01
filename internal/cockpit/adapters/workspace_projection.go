package adapters

import (
	"context"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/shares"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/platform/config"
)

func (a *LiveOperatorAdapter) workspaceFromView(ctx context.Context, workspace workspaceapi.Workspace) (readmodel.WorkspaceReadModel, error) {
	id := readmodel.WorkspaceID(workspace.Slug)
	activity, _ := a.activityForWorkspace(ctx, id)
	baseURL := a.workspaceURL(workspace.Slug)
	routes, err := routesFromWorkspace(workspace)
	if err != nil {
		return readmodel.WorkspaceReadModel{}, err
	}
	return readmodel.WorkspaceReadModel{ID: id, Slug: workspace.Slug, State: readmodel.WorkspaceExisting, WorkspaceURL: baseURL, Routes: routes, Activity: activity, ProviderOptions: operatorProviderOptions()}, nil
}

func projectWorkspaceShares(workspace readmodel.WorkspaceReadModel, summaries []shares.Summary) (readmodel.WorkspaceReadModel, error) {
	routes := append([]readmodel.RouteReadModel(nil), workspace.Routes...)
	for i := range routes {
		for _, summary := range summaries {
			if summary.Workspace != workspace.Slug || summary.Route != string(routes[i].ID) {
				continue
			}
			share := &readmodel.ShareReadModel{Hostname: summary.Hostname, Never: summary.ExpiresAt == nil}
			if summary.ExpiresAt != nil {
				expiresAt, err := time.Parse(time.RFC3339, *summary.ExpiresAt)
				if err != nil {
					return readmodel.WorkspaceReadModel{}, adapterFailure("decode share expiry", err)
				}
				share.ExpiresAt = expiresAt
			}
			routes[i].Share = share
		}
	}
	workspace.Routes = routes
	return workspace, nil
}
func (a *LiveOperatorAdapter) workspaceURL(slug string) string {
	return config.BaseURL(a.addr) + "/c/" + strings.Trim(strings.TrimSpace(slug), "/")
}
func (a *LiveOperatorAdapter) headerRight() string {
	if a.addr == "" || a.addr == config.DefaultAddr() {
		return ""
	}
	return a.addr
}
func draftWorkspace() readmodel.WorkspaceReadModel {
	return readmodel.NewDraftWorkspace(operatorProviderOptions())
}

func conventionalFirstWorkspace(endpoint string) readmodel.WorkspaceReadModel {
	return readmodel.NewConventionalFirstWorkspace(endpoint, operatorProviderOptions())
}
