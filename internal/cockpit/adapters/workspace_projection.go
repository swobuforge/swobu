package adapters

import (
	"context"
	"strings"

	clientprofile "github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/platform/config"
)

func (a *LiveOperatorAdapter) workspaceFromView(ctx context.Context, workspace workspaceapi.Workspace) (readmodel.WorkspaceReadModel, error) {
	id := readmodel.WorkspaceID(workspace.Slug)
	activity, _ := a.activityForWorkspace(ctx, id)
	baseURL := a.clientBaseURL(workspace.Slug)
	return readmodel.WorkspaceReadModel{ID: id, Slug: workspace.Slug, State: readmodel.WorkspaceExisting, ClientBaseURL: baseURL, RunCommands: runCommands(baseURL), Routes: routesFromWorkspace(workspace), Activity: activity, ProviderOptions: operatorProviderOptions()}, nil
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
func runCommands(baseURL string) []readmodel.RunCommandReadModel {
	profiles := clientprofile.Catalog()
	commands := make([]readmodel.RunCommandReadModel, 0, len(profiles))
	for _, profile := range profiles {
		identity := profile.Identity()
		actions := profile.Actions(baseURL)
		if len(actions) == 0 {
			continue
		}
		commands = append(commands, readmodel.RunCommandReadModel{ID: readmodel.RunCommandID(identity.ID), ClientID: readmodel.ClientID(identity.ID), Label: identity.Label, CommandName: identity.ID, TargetRouteID: readmodel.RouteID(exchange.PublicModelIDSwobu), TargetLabel: exchange.PublicModelIDSwobu, Effect: readmodel.RunCommandOpensClient, CommandPreview: actions[0].Content})
	}
	return commands
}
