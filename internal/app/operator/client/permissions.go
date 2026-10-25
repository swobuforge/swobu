package operatorclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// ReviewedPermission retains exact server-preview intent for a separately
// confirmed apply. Its private fields prevent edits after review. It is not
// authority or human approval; the trusted consumer must obtain confirmation.
type ReviewedPermission struct {
	workspace string
	principal string
	method    string
	revision  string
	key       string
	expiry    time.Time
}

// RestorePermissionIntent reconstructs an original uncertain request, not a
// fresh preview or approval. The trusted consumer must disclose original intent,
// inspect current state separately, and obtain fresh consent before applying it.
// Never supply a revision discovered during recovery in place of the original.
func RestorePermissionIntent(workspace, principal, operation, revision, key string, expiry time.Time) (ReviewedPermission, error) {
	if operation != "grant" && (!expiry.IsZero() || operation != "revoke") {
		return ReviewedPermission{}, errors.New("invalid original permission operation or expiry")
	}
	preview := accessapp.PermissionPreview{Workspace: workspace, Principal: principal, Operation: operation, Revision: revision, ApprovalClass: "trusted_confirmation", Warning: "Retained original intent is not approval"}
	if operation == "grant" {
		preview.After = &accessapp.Permission{Workspace: workspace, Action: "use"}
		if !expiry.IsZero() {
			value := expiry.UTC()
			preview.After.ExpiresAt = &value
		}
	}
	return ReviewPermission(preview, key)
}

// GetWorkspacePermission reads current relationship state for inspection or
// post-effect readback, never a substitute for the original reviewed consent.
func (c *Client) GetWorkspacePermission(ctx context.Context, workspace, principal string) (accessapp.PermissionState, error) {
	if _, err := routing.ParseWorkspaceSlug(workspace); err != nil {
		return accessapp.PermissionState{}, err
	}
	if _, err := accessidentity.RestoreManaged(principal); err != nil {
		return accessapp.PermissionState{}, err
	}
	var state accessapp.PermissionState
	if err := c.accessRequest(ctx, http.MethodGet, permissionPath(workspace, principal), nil, http.StatusOK, &state); err != nil {
		return accessapp.PermissionState{}, err
	}
	if state.Principal != principal || state.Workspace != workspace || state.Revision == "" || strings.ContainsAny(state.Revision, "\"\\, \t\r\n") {
		return accessapp.PermissionState{}, errors.New("permission read changed scope or omitted revision")
	}
	if state.Permission == nil {
		if state.Allowed {
			return accessapp.PermissionState{}, errors.New("absent permission reported allowed")
		}
	} else if state.Permission.Workspace != workspace || state.Permission.Action != "use" || (state.Permission.ExpiresAt != nil && state.Permission.ExpiresAt.IsZero()) {
		return accessapp.PermissionState{}, errors.New("permission read has invalid relationship")
	}
	return state, nil
}

// ReviewPermission binds a server preview to a stable retry key. It never
// refreshes revision, expands scope, or converts preview into approval.
func ReviewPermission(preview accessapp.PermissionPreview, key string) (ReviewedPermission, error) {
	if _, err := routing.ParseWorkspaceSlug(preview.Workspace); err != nil {
		return ReviewedPermission{}, err
	}
	if _, err := accessidentity.RestoreManaged(preview.Principal); err != nil {
		return ReviewedPermission{}, err
	}
	if preview.Revision == "" || strings.ContainsAny(preview.Revision, "\"\\, \t\r\n") || preview.ApprovalClass != "trusted_confirmation" || preview.Warning == "" || preview.AffectedCredentials < 0 {
		return ReviewedPermission{}, errors.New("permission preview is incomplete")
	}
	if key == "" || len(key) > 256 || strings.ContainsAny(key, " \t\r\n") {
		return ReviewedPermission{}, errors.New("stable permission retry key required")
	}
	result := ReviewedPermission{workspace: preview.Workspace, principal: preview.Principal, revision: preview.Revision, key: key}
	if preview.Before != nil && (preview.Before.Workspace != preview.Workspace || preview.Before.Action != "use" || (preview.Before.ExpiresAt != nil && preview.Before.ExpiresAt.IsZero())) {
		return ReviewedPermission{}, errors.New("permission preview has invalid prior relationship")
	}
	switch preview.Operation {
	case "grant":
		if preview.After == nil || preview.After.Workspace != preview.Workspace || preview.After.Action != "use" {
			return ReviewedPermission{}, errors.New("grant preview has invalid proposed permission")
		}
		result.method = http.MethodPut
		if preview.After.ExpiresAt != nil {
			if preview.After.ExpiresAt.IsZero() {
				return ReviewedPermission{}, errors.New("grant preview has zero expiry")
			}
			result.expiry = preview.After.ExpiresAt.UTC()
		}
	case "revoke":
		if preview.After != nil {
			return ReviewedPermission{}, errors.New("revoke preview retains permission")
		}
		result.method = http.MethodDelete
	default:
		return ReviewedPermission{}, errors.New("unsupported permission preview operation")
	}
	return result, nil
}

// PreviewWorkspacePermission returns effect-free server impact. Grant expiry
// is an instant; zero means no deadline. Revoke does not accept a deadline.
func (c *Client) PreviewWorkspacePermission(ctx context.Context, workspace, principal string, grant bool, expiry time.Time) (accessapp.PermissionPreview, error) {
	if _, err := routing.ParseWorkspaceSlug(workspace); err != nil {
		return accessapp.PermissionPreview{}, err
	}
	if _, err := accessidentity.RestoreManaged(principal); err != nil {
		return accessapp.PermissionPreview{}, err
	}
	if !grant && !expiry.IsZero() {
		return accessapp.PermissionPreview{}, errors.New("revoke cannot specify expiry")
	}
	method := http.MethodDelete
	if grant {
		method = http.MethodPut
	}
	var preview accessapp.PermissionPreview
	err := c.accessRequest(ctx, method, permissionPath(workspace, principal)+"?dry_run=true", permissionBody(grant, expiry), http.StatusOK, &preview)
	if err != nil {
		return accessapp.PermissionPreview{}, err
	}
	if preview.Workspace != workspace || preview.Principal != principal || (grant && preview.Operation != "grant") || (!grant && preview.Operation != "revoke") {
		return accessapp.PermissionPreview{}, errors.New("permission preview changed requested scope")
	}
	if _, err := ReviewPermission(preview, "preview-validation"); err != nil {
		return accessapp.PermissionPreview{}, err
	}
	if grant {
		var proposed time.Time
		if preview.After.ExpiresAt != nil {
			proposed = *preview.After.ExpiresAt
		}
		if !proposed.Equal(expiry) {
			return accessapp.PermissionPreview{}, errors.New("permission preview changed requested expiry")
		}
	}
	return preview, nil
}

// ApplyWorkspacePermission sends retained intent once, without discovery or
// automatic retry. An uncertain transport result requires inspection and a
// separately confirmed retry of this same intent, never refreshed consent.
func (c *Client) ApplyWorkspacePermission(ctx context.Context, reviewed ReviewedPermission) (accessapp.PermissionMutation, error) {
	if reviewed.method == "" || reviewed.revision == "" || reviewed.key == "" {
		return accessapp.PermissionMutation{}, errors.New("reviewed permission intent required")
	}
	// Presence is part of the result contract. Missing/null decisions cannot
	// default to false and turn an incomplete acknowledgement into success.
	var wire struct {
		Principal  string          `json:"principal"`
		Workspace  string          `json:"workspace"`
		Revision   string          `json:"revision"`
		Permission json.RawMessage `json:"permission"`
		Allowed    *bool           `json:"allowed"`
		Replayed   *bool           `json:"replayed"`
	}
	if err := c.accessRequestWithHeaders(ctx, reviewed.method, permissionPath(reviewed.workspace, reviewed.principal), permissionBody(reviewed.method == http.MethodPut, reviewed.expiry), http.StatusOK, &wire, map[string]string{"If-Match": `"` + reviewed.revision + `"`, "Idempotency-Key": reviewed.key}); err != nil {
		return accessapp.PermissionMutation{}, err
	}
	if wire.Allowed == nil || wire.Replayed == nil || len(wire.Permission) == 0 {
		return accessapp.PermissionMutation{}, errors.New("permission mutation result omitted explicit decisions")
	}
	result := accessapp.PermissionMutation{PermissionState: accessapp.PermissionState{Principal: wire.Principal, Workspace: wire.Workspace, Revision: wire.Revision, Allowed: *wire.Allowed}, Replayed: *wire.Replayed}
	if err := json.Unmarshal(wire.Permission, &result.Permission); err != nil {
		return accessapp.PermissionMutation{}, err
	}
	if result.Principal != reviewed.principal || result.Workspace != reviewed.workspace || result.Revision == "" || strings.ContainsAny(result.Revision, "\"\\, \t\r\n") {
		return accessapp.PermissionMutation{}, errors.New("permission mutation result changed scope or omitted revision")
	}
	if result.Permission == nil {
		if result.Allowed || (!result.Replayed && reviewed.method == http.MethodPut) {
			return accessapp.PermissionMutation{}, errors.New("permission mutation result contradicts applied grant")
		}
	} else {
		permission := result.Permission
		if permission.Workspace != reviewed.workspace || permission.Action != "use" || (permission.ExpiresAt != nil && permission.ExpiresAt.IsZero()) || (!result.Replayed && reviewed.method == http.MethodDelete) {
			return accessapp.PermissionMutation{}, errors.New("permission mutation result has invalid relationship")
		}
		if !result.Replayed && reviewed.method == http.MethodPut {
			var expiry time.Time
			if permission.ExpiresAt != nil {
				expiry = *permission.ExpiresAt
			}
			if !expiry.Equal(reviewed.expiry) {
				return accessapp.PermissionMutation{}, errors.New("permission mutation result changed expiry")
			}
		}
	}
	return result, nil
}

func permissionPath(workspace, principal string) string {
	return "/api/v1/workspaces/" + url.PathEscape(workspace) + "/permissions/" + url.PathEscape(principal)
}

func permissionBody(grant bool, expiry time.Time) any {
	if !grant {
		return struct{}{}
	}
	body := struct {
		Action    string     `json:"action"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}{Action: "use"}
	if !expiry.IsZero() {
		value := expiry.UTC()
		body.ExpiresAt = &value
	}
	return body
}
