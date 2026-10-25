package operatorclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/swobuforge/swobu/internal/accounting"
)

func (client *Client) LimitsStatus(ctx context.Context, workspace string) (accounting.Status, error) {
	var status accounting.Status
	err := client.workspaceRequest(ctx, http.MethodGet, "/api/v1/workspaces/"+url.PathEscape(workspace)+"/limits/status", nil, http.StatusOK, &status)
	return status, err
}
