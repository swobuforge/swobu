package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

func (c *Client) ListWorkspaces(ctx context.Context) ([]workspaceapi.WorkspaceSummary, error) {
	var out []workspaceapi.WorkspaceSummary
	err := c.workspaceRequest(ctx, http.MethodGet, c.operatorPath("/workspaces"), nil, http.StatusOK, &out)
	return out, err
}
func (c *Client) GetWorkspace(ctx context.Context, slug string) (workspaceapi.Workspace, error) {
	var out workspaceapi.Workspace
	err := c.workspaceRequest(ctx, http.MethodGet, c.operatorPath("/workspaces/")+url.PathEscape(strings.TrimSpace(slug)), nil, http.StatusOK, &out)
	return out, err
}

// PreviewMCPConnectionMount obtains the server-owned review for a Connection
// mount without mutating Workspace state.
func (c *Client) PreviewMCPConnectionMount(ctx context.Context, workspace, alias, connectionID string, selectedTools []string) (workspaceapi.MCPMountPreview, error) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(alias) == "" || !strings.HasPrefix(strings.TrimSpace(connectionID), "cn_") {
		return workspaceapi.MCPMountPreview{}, fmt.Errorf("operator client: canonical Workspace, alias and MCP Connection are required")
	}
	var out workspaceapi.MCPMountPreview
	body := map[string]any{"alias": alias, "connection_id": connectionID, "selected_tools": append([]string(nil), selectedTools...)}
	err := c.workspaceRequest(ctx, http.MethodPut, "/api/v1/workspaces/"+url.PathEscape(workspace)+"/mcp-mounts/"+url.PathEscape(alias)+"?dry_run=true", body, http.StatusOK, &out)
	return out, err
}

// ApplyMCPConnectionMount commits the reviewed mount against the current
// Workspace ETag. On an uncertain response, read the Workspace before retrying.
func (c *Client) ApplyMCPConnectionMount(ctx context.Context, workspace, alias, connectionID string, selectedTools []string, review workspaceapi.MCPMountReviewRequest) (workspaceapi.Workspace, error) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(alias) == "" || !strings.HasPrefix(strings.TrimSpace(connectionID), "cn_") {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: reviewed canonical MCP mount intent is required")
	}
	body := map[string]any{"alias": alias, "connection_id": connectionID, "selected_tools": append([]string(nil), selectedTools...), "review": review}
	raw, err := json.Marshal(body)
	if err != nil {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: MCP mount payload could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/api/v1/workspaces/"+url.PathEscape(workspace)+"/mcp-mounts/"+url.PathEscape(alias)+"?dry_run=false", bytes.NewReader(raw))
	if err != nil {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: MCP mount request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+review.WorkspaceETag+`"`)
	resp, err := c.http.Do(req)
	if err != nil {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: MCP mount outcome is unknown")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return workspaceapi.Workspace{}, errorFromResponse(resp, "operator client: MCP mount failed")
	}
	var result workspaceapi.Workspace
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.Slug == "" {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: invalid MCP mount acknowledgement")
	}
	return result, nil
}

// PreviewWorkspaceDeletion returns server-owned advisory impact. It does not
// confirm a change; the caller must display it and obtain human confirmation.
func (c *Client) PreviewWorkspaceDeletion(ctx context.Context, slug string) (workspaceapi.DeletionPreview, error) {
	var out workspaceapi.DeletionPreview
	err := c.workspaceRequest(ctx, http.MethodDelete, "/api/v1/workspaces/"+url.PathEscape(strings.TrimSpace(slug))+"?dry_run=true", nil, http.StatusOK, &out)
	return out, err
}

// ApplyWorkspaceDeletion sends the exact reviewed lifetime/revision and stable
// retry key. Callers must not interpret a readback failure as rollback, or retry
// with a new key after an uncertain response.
func (c *Client) ApplyWorkspaceDeletion(ctx context.Context, slug, etag, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/v1/workspaces/"+url.PathEscape(strings.TrimSpace(slug)), strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("operator client: deletion request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+etag+`"`)
	req.Header.Set("Idempotency-Key", key)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("operator client: deletion unavailable; inspect state and retry the same key and ETag")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errorFromResponse(resp, "operator client: deletion failed")
	}
	var result struct {
		Deleted bool `json:"deleted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || !result.Deleted {
		return fmt.Errorf("operator client: deletion outcome uncertain; inspect state and retry the same key and ETag")
	}
	return nil
}
func (c *Client) GetRoute(ctx context.Context, workspace, route string) (workspaceapi.RouteSpec, error) {
	var out workspaceapi.RouteSpec
	err := c.workspaceRequest(ctx, http.MethodGet, c.workspacePath(workspace)+"/model-routes/"+url.PathEscape(strings.TrimSpace(route)), nil, http.StatusOK, &out)
	return out, err
}

// PutModelRoute replaces the full route under the Workspace's current
// revision. On an uncertain response, read the Workspace before retrying.
func (c *Client) PutModelRoute(ctx context.Context, command workspaceapi.ReplaceRoute, workspaceETag string) (workspaceapi.Workspace, error) {
	if workspaceETag == "" || strings.TrimSpace(command.Workspace) == "" || strings.TrimSpace(command.Route) == "" {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: Workspace, route and ETag required")
	}
	body, err := json.Marshal(command.Spec)
	if err != nil {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: route payload could not be encoded")
	}
	path := c.workspacePath(command.Workspace) + "/model-routes/" + url.PathEscape(command.Route)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: route request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+workspaceETag+`"`)
	resp, err := c.http.Do(req)
	if err != nil {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: route write outcome uncertain; inspect Workspace before retrying")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return workspaceapi.Workspace{}, errorFromResponse(resp, "operator client: route write failed")
	}
	var workspace workspaceapi.Workspace
	if err := json.NewDecoder(resp.Body).Decode(&workspace); err != nil || workspace.Slug == "" {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: invalid route acknowledgement; inspect Workspace")
	}
	return workspace, nil
}

func (c *Client) workspacePath(slug string) string {
	return c.operatorPath("/workspaces/") + url.PathEscape(strings.TrimSpace(slug))
}
func (c *Client) workspaceRequest(ctx context.Context, method, path string, body any, want int, out any) error {
	var encoded = []byte("{}")
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("operator client: command payload could not be encoded")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("operator client: command request could not be built")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("operator client: workspace command is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return errorFromResponse(resp, "operator client: workspace command failed")
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("operator client: workspace response could not be decoded")
		}
	}
	return nil
}

// SetSearchRoute commits the complete ordered fallback chain at the latest
// Workspace ETag. A concurrent edit fails; no blind overwrite is permitted.
func (c *Client) SetSearchRoute(ctx context.Context, workspace, primary string, fallback []string) (workspaceapi.Workspace, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || !strings.HasPrefix(strings.TrimSpace(primary), "cn_") {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: canonical Workspace and Search Connection are required")
	}
	current, err := c.GetWorkspace(ctx, workspace)
	if err != nil {
		return workspaceapi.Workspace{}, err
	}
	body := map[string]any{"connection_id": primary, "fallback_connection_ids": append([]string{}, fallback...)}
	raw, err := json.Marshal(body)
	if err != nil {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: Search route payload could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/api/v1/workspaces/"+url.PathEscape(workspace)+"/search-route", bytes.NewReader(raw))
	if err != nil {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: Search route request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+current.ManagementETag+`"`)
	resp, err := c.http.Do(req)
	if err != nil {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: Search route outcome is unknown")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return workspaceapi.Workspace{}, errorFromResponse(resp, "operator client: Search route failed")
	}
	var out workspaceapi.Workspace
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Slug == "" {
		return workspaceapi.Workspace{}, fmt.Errorf("operator client: invalid Search route acknowledgement")
	}
	return out, nil
}
