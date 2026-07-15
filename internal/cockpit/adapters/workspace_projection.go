package adapters

import (
	"context"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/exchange"
)

func (a *LiveOperatorAdapter) workspaceFromEndpoint(ctx context.Context, endpoint operatorclient.EndpointData) (readmodel.WorkspaceReadModel, error) {
	workspaceID := readmodel.WorkspaceID(endpoint.Name)
	activity, _ := a.activityForWorkspace(ctx, workspaceID)
	baseURL := a.clientBaseURL(endpoint.Name)
	return readmodel.WorkspaceReadModel{
		ID:            workspaceID,
		Slug:          endpoint.Name,
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: baseURL,
		RunCommands:   runCommands(baseURL),
		Routes:        routesFromEndpoint(endpoint),
		Activity:      activity,
	}, nil
}

func (a *LiveOperatorAdapter) clientBaseURL(slug string) string {
	return a.daemonURL + "/c/" + strings.Trim(strings.TrimSpace(slug), "/")
}

func (a *LiveOperatorAdapter) environmentLabel() string {
	if a.daemonURL == "" {
		return "local"
	}
	return a.daemonURL
}

func draftWorkspace() readmodel.WorkspaceReadModel {
	return readmodel.WorkspaceReadModel{
		ID:    "+",
		State: readmodel.WorkspaceDraft,
	}
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
		action := actions[0]
		commands = append(commands, readmodel.RunCommandReadModel{
			ID:             readmodel.RunCommandID(identity.ID),
			ClientID:       readmodel.ClientID(identity.ID),
			Label:          identity.Label,
			CommandName:    identity.ID,
			TargetRouteID:  readmodel.RouteID(exchange.PublicModelIDSwobu),
			TargetLabel:    exchange.PublicModelIDSwobu,
			Effect:         readmodel.RunCommandOpensClient,
			CommandPreview: action.Content,
		})
	}
	return commands
}
