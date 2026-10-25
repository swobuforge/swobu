package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/routing"
)

// WorkspaceCreationIntent retains the exact request bytes and retry key for
// explicit recovery. It carries no approval; trusted consumers own confirmation.
type WorkspaceCreationIntent struct {
	body                   []byte
	slug, name, route, key string
}

// NewWorkspaceCreationIntent freezes caller intent; apply cannot substitute a
// different draft or generate a replacement key after an uncertain response.
func NewWorkspaceCreationIntent(cmd workspaceapi.CreateWorkspace, key string) (WorkspaceCreationIntent, error) {
	slug, err := routing.ParseWorkspaceSlug(cmd.Slug)
	if err != nil || slug.String() != cmd.Slug {
		return WorkspaceCreationIntent{}, errors.New("exact Workspace slug required")
	}
	w, err := routing.RestoreWorkspace(slug, nil, nil)
	if err != nil {
		return WorkspaceCreationIntent{}, err
	}
	w, err = w.WithDisplayName(cmd.DisplayName)
	if err != nil || w.DisplayName() != cmd.DisplayName {
		return WorkspaceCreationIntent{}, errors.New("exact valid display name required")
	}
	if len(key) == 0 || len(key) > 256 {
		return WorkspaceCreationIntent{}, errors.New("creation retry key required")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return WorkspaceCreationIntent{}, errors.New("invalid creation retry key")
		}
	}
	var body any = cmd
	if cmd.InitialRoute == "" {
		body = struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name,omitempty"`
		}{cmd.Slug, cmd.DisplayName}
	} else {
		route, err := routing.ParseRouteName(cmd.InitialRoute)
		if err != nil || route.String() != cmd.InitialRoute {
			return WorkspaceCreationIntent{}, errors.New("exact initial Route required")
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return WorkspaceCreationIntent{}, errors.New("invalid Workspace creation payload")
	}
	return WorkspaceCreationIntent{body: raw, slug: cmd.Slug, name: cmd.DisplayName, route: cmd.InitialRoute, key: key}, nil
}

// ApplyWorkspaceCreation sends the retained intent once to canonical Control.
// A replay must acknowledge the original result, not refreshed current state.
func (c *Client) ApplyWorkspaceCreation(ctx context.Context, intent WorkspaceCreationIntent) (workspaceapi.Workspace, bool, error) {
	if len(intent.body) == 0 || intent.key == "" {
		return workspaceapi.Workspace{}, false, errors.New("retained creation intent required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/workspaces", bytes.NewReader(intent.body))
	if err != nil {
		return workspaceapi.Workspace{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", intent.key)
	req.GetBody = nil
	response, err := c.http.Do(req)
	if err != nil {
		return workspaceapi.Workspace{}, false, errors.New("Workspace creation not acknowledged; outcome may have committed; retain original intent and key for confirmed recovery")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return workspaceapi.Workspace{}, false, errorFromResponse(response, "Workspace creation failed")
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(response.Body)
	if decoder.Decode(&fields) != nil || fields == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return workspaceapi.Workspace{}, false, errors.New("invalid creation acknowledgement; outcome may have committed")
	}
	var name *string
	var workspace workspaceapi.Workspace
	for _, member := range []string{"readiness", "management_revision", "management_etag"} {
		if len(fields[member]) == 0 || string(fields[member]) == "null" {
			return workspaceapi.Workspace{}, false, errors.New("creation acknowledgement omits required metadata; outcome may have committed")
		}
	}
	if len(fields["routes"]) == 0 {
		return workspaceapi.Workspace{}, false, errors.New("creation acknowledgement omits Route projection")
	}
	raw, err := json.Marshal(fields)
	if err != nil || json.Unmarshal(fields["display_name"], &name) != nil || name == nil || *name != intent.name || json.Unmarshal(raw, &workspace) != nil || workspace.Slug != intent.slug || workspace.ManagementRevision != 1 || workspace.ManagementETag == "" || strings.ContainsAny(workspace.ManagementETag, "\"\\, \t\r\n") || response.Header.Get("ETag") != `"`+workspace.ManagementETag+`"` || response.Header.Get("Location") != "/api/v1/workspaces/"+url.PathEscape(intent.slug) {
		return workspaceapi.Workspace{}, false, errors.New("creation acknowledgement contradicts original intent; outcome may have committed")
	}
	if intent.route == "" && (len(workspace.Routes) != 0 || (workspace.Readiness.LLM != "disabled" && workspace.Readiness.LLM != "not_configured") || (workspace.Readiness.MCP != "disabled" && workspace.Readiness.MCP != "not_configured")) {
		return workspaceapi.Workspace{}, false, errors.New("empty creation acknowledgement fabricated capability")
	}
	if intent.route != "" && (len(workspace.Routes) != 1 || workspace.Routes[0].Name != intent.route) {
		return workspaceapi.Workspace{}, false, errors.New("creation acknowledgement changed initial Route")
	}
	return workspace, response.StatusCode == http.StatusOK, nil
}

// PreviewWorkspaceCreation sends the retained apply body without consuming its
// retry key. A preview is advisory; it neither approves nor reserves creation.
func (c *Client) PreviewWorkspaceCreation(ctx context.Context, intent WorkspaceCreationIntent) (workspaceapi.CreationPreview, error) {
	if len(intent.body) == 0 {
		return workspaceapi.CreationPreview{}, errors.New("retained creation intent required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/workspaces?dry_run=true", bytes.NewReader(intent.body))
	if err != nil {
		return workspaceapi.CreationPreview{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = nil
	response, err := c.http.Do(req)
	if err != nil {
		return workspaceapi.CreationPreview{}, errors.New("Workspace creation preview unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return workspaceapi.CreationPreview{}, errorFromResponse(response, "Workspace creation preview failed")
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(response.Body)
	if decoder.Decode(&fields) != nil || fields == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return workspaceapi.CreationPreview{}, errors.New("invalid creation preview")
	}
	for _, forbidden := range []string{"management_etag", "management_revision", "credential", "connection", "token", "secret"} {
		if _, exists := fields[forbidden]; exists {
			return workspaceapi.CreationPreview{}, errors.New("creation preview contains authority or credential material")
		}
	}
	for _, required := range []string{"slug", "display_name", "route_count", "target_count", "readiness", "approval_class", "diff", "warning"} {
		if len(fields[required]) == 0 || string(fields[required]) == "null" {
			return workspaceapi.CreationPreview{}, errors.New("creation preview omits reviewed impact")
		}
	}
	var preview workspaceapi.CreationPreview
	raw, err := json.Marshal(fields)
	if err != nil || json.Unmarshal(raw, &preview) != nil || preview.Slug != intent.slug || preview.DisplayName != intent.name || preview.ApprovalClass != "confirmation" || preview.Diff == "" || preview.Warning == "" || response.Header.Get("ETag") != "" {
		return workspaceapi.CreationPreview{}, errors.New("creation preview contradicts retained intent")
	}
	count := 0
	if intent.route != "" {
		count = 1
	}
	if preview.RouteCount != count || preview.TargetCount != count || (preview.Readiness.MCP != "disabled" && preview.Readiness.MCP != "not_configured") || (count == 0 && preview.Readiness.LLM != "disabled" && preview.Readiness.LLM != "not_configured") {
		return workspaceapi.CreationPreview{}, errors.New("creation preview contradicts initial readiness or routing")
	}
	return preview, nil
}
