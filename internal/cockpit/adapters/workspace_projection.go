package adapters

import (
	"context"
	"strings"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/platform/config"
)

func (a *LiveOperatorAdapter) workspaceFromView(ctx context.Context, workspace workspaceapi.Workspace) (readmodel.WorkspaceReadModel, error) {
	id := readmodel.WorkspaceID(workspace.Slug)
	activity, _ := a.activityForWorkspace(ctx, id)
	baseURL := a.clientBaseURL(workspace.Slug)
	return readmodel.WorkspaceReadModel{ID: id, Slug: workspace.Slug, State: readmodel.WorkspaceExisting, ClientBaseURL: baseURL, Routes: routesFromWorkspace(workspace), Activity: activity, ProviderOptions: operatorProviderOptions()}, nil
}
func (a *LiveOperatorAdapter) clientBaseURL(slug string) string {
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
