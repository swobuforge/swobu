package operatorclient

import (
	"context"
	"net/http"
)

// NodeStatus is the redacted local Control health projection, not traffic,
// credential authority or evidence of provider readiness.
type NodeStatus struct {
	State                string `json:"state"`
	WorkspaceCount       int    `json:"workspace_count"`
	ControlPlaneProtocol int    `json:"control_plane_protocol"`
	SwobuVersion         string `json:"swobu_version"`
}

// InspectNode reads canonical Node status once without legacy fallback.
func (client *Client) InspectNode(ctx context.Context) (NodeStatus, error) {
	var status NodeStatus
	err := client.workspaceRequest(ctx, http.MethodGet, "/api/v1/status", nil, http.StatusOK, &status)
	return status, err
}
