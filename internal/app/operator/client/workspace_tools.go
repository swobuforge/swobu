package operatorclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/swobuforge/swobu/internal/routing"
)

// WorkspaceTool is derived catalog identity metadata, not an invocation binding
// or credential authority. Native schemas remain on the Workspace MCP surface.
type WorkspaceTool struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	RemoteName string `json:"remote_name"`
}

// ConnectionMCPTool is the secret-free upstream declaration projection used
// while authoring a Connection-backed MCP mount.
type ConnectionMCPTool struct {
	Name               string `json:"name"`
	RemoteName         string `json:"remote_name"`
	ConnectionID       string `json:"connection_id"`
	ConnectionRevision string `json:"connection_revision"`
}

// ListConnectionMCPTools reads the current revision-bound MCP catalog from the
// canonical Connection API without falling back to source registries.
func (client *Client) ListConnectionMCPTools(ctx context.Context, connectionID string) ([]ConnectionMCPTool, error) {
	if connectionID == "" {
		return nil, errors.New("Connection ID is required")
	}
	var tools []ConnectionMCPTool
	if err := client.workspaceRequest(ctx, http.MethodGet, "/api/v1/connections/"+url.PathEscape(connectionID)+"/mcp-catalog", nil, http.StatusOK, &tools); err != nil {
		return nil, err
	}
	if tools == nil {
		return nil, errors.New("missing Connection MCP catalog acknowledgement")
	}
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if tool.Name == "" || tool.RemoteName == "" || tool.ConnectionID != connectionID || tool.ConnectionRevision == "" || seen[tool.Name] {
			return nil, errors.New("invalid Connection MCP catalog acknowledgement")
		}
		seen[tool.Name] = true
	}
	return tools, nil
}

// ListWorkspaceTools reads the effective local catalog without source refresh,
// execution or fallback to a Workspace data endpoint.
func (client *Client) ListWorkspaceTools(ctx context.Context, workspace string) ([]WorkspaceTool, error) {
	slug, err := routing.ParseWorkspaceSlug(workspace)
	if err != nil || slug.String() != workspace {
		return nil, errors.New("exact Workspace slug required")
	}
	var tools []WorkspaceTool
	if err := client.workspaceRequest(ctx, http.MethodGet, "/api/v1/workspaces/"+url.PathEscape(workspace)+"/tools", nil, http.StatusOK, &tools); err != nil {
		return nil, err
	}
	if tools == nil {
		return nil, errors.New("missing Workspace catalog acknowledgement")
	}
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if tool.Name == "" || tool.Source == "" || tool.RemoteName == "" || seen[tool.Name] {
			return nil, errors.New("invalid Workspace catalog acknowledgement")
		}
		seen[tool.Name] = true
	}
	return tools, nil
}
