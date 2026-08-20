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
	err := c.workspaceRequest(ctx, http.MethodGet, "/_swobu/workspaces", nil, http.StatusOK, &out)
	return out, err
}
func (c *Client) GetWorkspace(ctx context.Context, slug string) (workspaceapi.Workspace, error) {
	var out workspaceapi.Workspace
	err := c.workspaceRequest(ctx, http.MethodGet, "/_swobu/workspaces/"+url.PathEscape(strings.TrimSpace(slug)), nil, http.StatusOK, &out)
	return out, err
}
func (c *Client) CreateWorkspace(ctx context.Context, cmd workspaceapi.CreateWorkspace) (workspaceapi.Workspace, error) {
	var out workspaceapi.Workspace
	err := c.workspaceRequest(ctx, http.MethodPost, "/_swobu/workspaces", cmd, http.StatusCreated, &out)
	return out, err
}
func (c *Client) RenameWorkspace(ctx context.Context, slug, newSlug string) (workspaceapi.Workspace, error) {
	var out workspaceapi.Workspace
	err := c.workspaceRequest(ctx, http.MethodPost, workspacePath(slug)+"/rename", map[string]string{"new_slug": newSlug}, http.StatusOK, &out)
	return out, err
}
func (c *Client) DeleteWorkspace(ctx context.Context, slug string) error {
	return c.workspaceRequest(ctx, http.MethodDelete, workspacePath(slug), nil, http.StatusNoContent, nil)
}
func (c *Client) CreateRoute(ctx context.Context, cmd workspaceapi.CreateRoute) (workspaceapi.Workspace, error) {
	var out workspaceapi.Workspace
	err := c.workspaceRequest(ctx, http.MethodPost, workspacePath(cmd.Workspace)+"/routes", cmd, http.StatusOK, &out)
	return out, err
}
func (c *Client) RenameRoute(ctx context.Context, cmd workspaceapi.RenameRoute) (workspaceapi.Workspace, error) {
	var out workspaceapi.Workspace
	err := c.workspaceRequest(ctx, http.MethodPost, routePath(cmd.Workspace, cmd.Route)+"/rename", map[string]string{"new_name": cmd.NewName}, http.StatusOK, &out)
	return out, err
}
func (c *Client) SetDefaultRoute(ctx context.Context, cmd workspaceapi.SetDefaultRoute) (workspaceapi.Workspace, error) {
	var out workspaceapi.Workspace
	err := c.workspaceRequest(ctx, http.MethodPost, workspacePath(cmd.Workspace)+"/default-route", map[string]string{"route": cmd.Route}, http.StatusOK, &out)
	return out, err
}
func (c *Client) DeleteRoute(ctx context.Context, cmd workspaceapi.DeleteRoute) (workspaceapi.Workspace, error) {
	var out workspaceapi.Workspace
	body := map[string]string{}
	if cmd.Replacement != "" {
		body["replacement"] = cmd.Replacement
	}
	err := c.workspaceRequest(ctx, http.MethodDelete, routePath(cmd.Workspace, cmd.Route), body, http.StatusOK, &out)
	return out, err
}
func (c *Client) GetRoute(ctx context.Context, workspace, route string) (workspaceapi.RouteSpec, error) {
	var out workspaceapi.RouteSpec
	err := c.workspaceRequest(ctx, http.MethodGet, routePath(workspace, route), nil, http.StatusOK, &out)
	return out, err
}
func (c *Client) ReplaceRoute(ctx context.Context, cmd workspaceapi.ReplaceRoute) (workspaceapi.Workspace, error) {
	var out workspaceapi.Workspace
	err := c.workspaceRequest(ctx, http.MethodPut, routePath(cmd.Workspace, cmd.Route), cmd.Spec, http.StatusOK, &out)
	return out, err
}

func workspacePath(slug string) string {
	return "/_swobu/workspaces/" + url.PathEscape(strings.TrimSpace(slug))
}
func routePath(slug, route string) string {
	return workspacePath(slug) + "/routes/" + url.PathEscape(strings.TrimSpace(route))
}
func (c *Client) workspaceRequest(ctx context.Context, method, path string, body any, want int, out any) error {
	var encoded []byte
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
	if body != nil {
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
