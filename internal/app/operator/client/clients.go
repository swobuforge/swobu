package operatorclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/routing"
)

// ListLocalClients reads presence-only Connect discovery, not effective settings
// or client configuration contents. It never runs or configures a client.
func (client *Client) ListLocalClients(ctx context.Context, workspace string) ([]connectapp.Client, error) {
	slug, err := routing.ParseWorkspaceSlug(workspace)
	if err != nil || slug.String() != workspace {
		return nil, errors.New("exact Workspace slug required")
	}
	var clients []connectapp.Client
	if err := client.workspaceRequest(ctx, http.MethodGet, "/api/v1/clients?workspace="+url.QueryEscape(workspace), nil, http.StatusOK, &clients); err != nil {
		return nil, err
	}
	if clients == nil {
		return nil, errors.New("missing client discovery acknowledgement")
	}
	seen := make(map[string]bool, len(clients))
	for _, discovered := range clients {
		if _, err := clientconnect.ParseClientID(discovered.ID); err != nil || discovered.Name == "" || seen[discovered.ID] {
			return nil, errors.New("invalid client discovery acknowledgement")
		}
		seen[discovered.ID] = true
	}
	return clients, nil
}
