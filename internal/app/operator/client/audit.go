package operatorclient

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/swobuforge/swobu/internal/accessstate"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/clientconnect"

	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// AuditEvents reads private retained management facts once. It rejects malformed
// scope or acknowledgements without retry, fallback or disclosure of denial text.
func (client *Client) AuditEvents(ctx context.Context, workspace string) (managementaudit.Snapshot, error) {
	slug, err := routing.ParseWorkspaceSlug(workspace)
	if err != nil || slug.String() != workspace {
		return managementaudit.Snapshot{}, fmt.Errorf("operator client: exact audit Workspace required")
	}
	return client.readAuditEvents(ctx, workspace)
}

// NodeAuditEvents explicitly selects private Node-wide inspection, including
// actions without Workspace impact. It never fabricates Workspace attribution.
func (client *Client) NodeAuditEvents(ctx context.Context) (managementaudit.Snapshot, error) {
	return client.readAuditEvents(ctx, "")
}

func (client *Client) readAuditEvents(ctx context.Context, workspace string) (managementaudit.Snapshot, error) {
	path := "/api/v1/audit/events"
	if workspace != "" {
		path += "?workspace=" + url.QueryEscape(workspace)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return managementaudit.Snapshot{}, err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return managementaudit.Snapshot{}, fmt.Errorf("operator client: audit read unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return managementaudit.Snapshot{}, &ResponseError{StatusCode: response.StatusCode, Fallback: "operator client: audit read failed"}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
	if err != nil || len(raw) > 8<<20 {
		return managementaudit.Snapshot{}, fmt.Errorf("operator client: audit response unavailable or too large")
	}
	var envelope struct {
		Events []map[string]json.RawMessage `json:"events"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid audit acknowledgement")
	}
	for _, event := range envelope.Events {
		if value, exists := event["connection"]; exists {
			var action struct {
				Action string `json:"action"`
			}
			if json.Unmarshal(value, &action) != nil {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid Connection facts")
			}
			count := 5
			if action.Action == "connection.edit" || action.Action == "connection.credential_replace" {
				count = 6
			}
			fields, err := decodeAuditFactFields(value, count)
			if err != nil {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid Connection facts")
			}
			for name, value := range fields {
				switch name {
				case "action", "caller", "connection_id", "before_revision", "after_revision", "committed_at":
				default:
					return managementaudit.Snapshot{}, fmt.Errorf("operator client: unknown Connection fact")
				}
				if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					return managementaudit.Snapshot{}, fmt.Errorf("operator client: null Connection fact")
				}
			}
			for _, name := range []string{"action", "caller", "connection_id", "committed_at"} {
				if _, exists := fields[name]; !exists {
					return managementaudit.Snapshot{}, fmt.Errorf("operator client: incomplete Connection facts")
				}
			}
		}

		if value, exists := event["workspace_display_name"]; exists {
			fields, err := decodeAuditFactFields(value, 7)
			if err != nil {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid display name facts")
			}
			for _, name := range []string{"action", "caller", "workspace", "etag", "before", "after", "committed_at"} {
				value, present := fields[name]
				if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					return managementaudit.Snapshot{}, fmt.Errorf("operator client: incomplete display name facts")
				}
			}
		}
	}
	var result managementaudit.Snapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	expectedFamilies := []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"}
	if decoder.Decode(&result) != nil || decoder.Decode(&struct{}{}) != io.EOF || result.Workspace != workspace || result.Coverage != "partial" || !slices.Equal(result.CaptureFamilies, expectedFamilies) || result.Events == nil {
		return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid audit acknowledgement")
	}
	for _, event := range result.Events {
		facts := 0
		if event.Connection != nil {
			facts++
		}
		if event.WorkspaceDisplayName != nil {
			facts++
		}
		if event.WorkspaceCreation != nil {
			facts++
		}
		if event.WorkspaceDeletion != nil {
			facts++
		}
		if event.ClientConfiguration != nil {
			facts++
		}
		if event.PrincipalLabel != nil {
			facts++
		}
		if event.PrincipalCreation != nil {
			facts++
		}
		if event.Permission != nil {
			facts++
		}
		if event.Limit != nil {
			facts++
		}
		if event.PrincipalRevocation != nil {
			facts++
		}
		if event.CredentialRevocation != nil {
			facts++
		}
		if event.CredentialIssuance != nil {
			facts++
		}
		if (workspace != "" && event.Workspace != workspace) || event.Actor == "" || event.CommittedAt.IsZero() || facts != 1 {
			return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid audit event binding")
		}
		if audit := event.Connection; audit != nil {
			if workspace != "" || event.Workspace != "" || audit.Caller != event.Actor || audit.Action != event.Action || !audit.CommittedAt.Equal(event.CommittedAt) || audit.Validate() != nil {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid Connection audit binding")
			}
		}
		if audit := event.WorkspaceDisplayName; audit != nil {
			slug, scopeErr := routing.ParseWorkspaceSlug(audit.Workspace)
			identity, revision, found := strings.Cut(audit.ETag, "-")
			decoded, identityErr := hex.DecodeString(identity)
			number, revisionErr := strconv.ParseUint(revision, 10, 64)
			if scopeErr != nil || slug.String() != audit.Workspace || !found || identityErr != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != identity || revisionErr != nil || number == 0 || strconv.FormatUint(number, 10) != revision || event.Workspace != audit.Workspace || !validManagementActor(event.Actor) || audit.Caller != event.Actor || event.Action != "workspace.display_name" || audit.Action != event.Action || !audit.CommittedAt.Equal(event.CommittedAt) || audit.Before == audit.After {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid Workspace display name audit binding")
			}
			workspace, err := routing.NewWorkspace(slug, nil)
			if err != nil {
				return managementaudit.Snapshot{}, err
			}
			for _, name := range []string{audit.Before, audit.After} {
				validated, err := workspace.WithDisplayName(name)
				if err != nil || validated.DisplayName() != name {
					return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid audited display name")
				}
			}
		}
		if audit := event.WorkspaceCreation; audit != nil {
			slug, scopeErr := routing.ParseWorkspaceSlug(audit.Workspace)
			identity, identityErr := hex.DecodeString(audit.Incarnation)
			if scopeErr != nil || slug.String() != audit.Workspace || identityErr != nil || len(identity) != 32 || hex.EncodeToString(identity) != audit.Incarnation || event.Workspace != audit.Workspace || event.Actor != audit.Caller || audit.Caller == "" || event.Action != "workspace.create" || audit.Action != event.Action || !audit.CommittedAt.Equal(event.CommittedAt) {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid Workspace creation audit binding")
			}
		}
		if audit := event.WorkspaceDeletion; audit != nil {
			slug, scopeErr := routing.ParseWorkspaceSlug(audit.Workspace)
			identity, revision, found := strings.Cut(audit.ETag, "-")
			decoded, identityErr := hex.DecodeString(identity)
			number, revisionErr := strconv.ParseUint(revision, 10, 64)
			if scopeErr != nil || slug.String() != audit.Workspace || !found || identityErr != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != identity || revisionErr != nil || number == 0 || strconv.FormatUint(number, 10) != revision || event.Workspace != audit.Workspace || event.Actor != audit.Caller || audit.Caller == "" || event.Action != "workspace.delete" || audit.Action != event.Action || !audit.CommittedAt.Equal(event.CommittedAt) {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid Workspace deletion audit binding")
			}
		}
		if audit := event.ClientConfiguration; audit != nil {
			slug, scopeErr := routing.ParseWorkspaceSlug(audit.Workspace)
			clientID, clientErr := clientconnect.ParseClientID(audit.Client)
			revision, revisionErr := hex.DecodeString(strings.TrimPrefix(audit.Revision, "connect-"))
			if scopeErr != nil || slug.String() != audit.Workspace || clientErr != nil || string(clientID) != audit.Client || revisionErr != nil || len(revision) != 32 || audit.Revision != "connect-"+hex.EncodeToString(revision) || event.Workspace != audit.Workspace || event.Actor != "local" || audit.Caller != event.Actor || event.Action != "client_configuration.apply" || audit.Action != event.Action || !audit.CommittedAt.Equal(event.CommittedAt) {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid client configuration audit binding")
			}
		}
		if permission := event.Permission; permission != nil {
			principal, err := accessidentity.Parse(permission.Principal)
			if err != nil || principal.String() != permission.Principal {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid audit Principal")
			}
			slug, scopeErr := routing.ParseWorkspaceSlug(event.Workspace)
			if scopeErr != nil || slug.String() != event.Workspace || !validManagementActor(event.Actor) || permission.Workspace != event.Workspace || permission.Action != event.Action || (event.Action != "permission.grant" && event.Action != "permission.revoke") || permission.Principal == "" || permission.Revision == "" || !permission.CommittedAt.Equal(event.CommittedAt) || (event.Action == "permission.revoke" && !permission.ExpiresAt.IsZero()) {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid permission audit binding")
			}
		}
		if audit := event.PrincipalCreation; audit != nil {
			principal, err := accessidentity.Parse(audit.Principal)
			if err != nil || !principal.Managed() || workspace != "" || event.Workspace != "" || event.Action != "principal.create" || !validManagementActor(event.Actor) || audit.Caller != event.Actor || audit.Label == "" || !audit.CommittedAt.Equal(event.CommittedAt) {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid Principal creation audit")
			}
		}
		if audit := event.PrincipalLabel; audit != nil {
			principal, err := accessidentity.RestoreManaged(audit.Principal)
			if err != nil || workspace != "" || event.Workspace != "" || event.Action != "principal.label" || !validManagementActor(event.Actor) || audit.Caller != event.Actor || audit.Before == "" || audit.After == "" || audit.Before == audit.After || audit.Revision != accessstate.PrincipalRevision(accessstate.Access{ID: principal, AuthorityGeneration: audit.Generation}) || !audit.CommittedAt.Equal(event.CommittedAt) {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid Principal label audit")
			}
		}
		if limit := event.Limit; limit != nil {
			slug, scopeErr := routing.ParseWorkspaceSlug(event.Workspace)
			if scopeErr != nil || slug.String() != event.Workspace || event.Action != "limit.apply" || limit.Workspace != event.Workspace || limit.Actor != event.Actor || !validManagementActor(limit.Actor) || !limit.AppliedAt.Equal(event.CommittedAt) || limit.ApprovalID == "" || limit.Revision == "" || limit.Digest == "" || limit.After.Validate() != nil || (limit.Before != nil && limit.Before.Validate() != nil) {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid Limits audit binding")
			}
		}
		if audit := event.PrincipalRevocation; audit != nil {
			if workspace == "" && event.Workspace != "" {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: Node revocation has fabricated Workspace attribution")
			}
			principal, err := accessidentity.Parse(audit.Principal)
			if err != nil || !principal.Managed() || event.Action != "principal.revoke" || event.Actor != "local" || audit.Caller != event.Actor || audit.Revision == "" || !audit.CommittedAt.Equal(event.CommittedAt) || audit.Workspaces == nil {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid Principal revocation audit")
			}
			found := false
			for index, affected := range audit.Workspaces {
				slug, err := routing.ParseWorkspaceSlug(affected)
				if err != nil || slug.String() != affected || (index > 0 && audit.Workspaces[index-1] >= affected) {
					return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid revocation audit scope")
				}
				found = found || affected == workspace
			}
			if workspace != "" && !found {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: foreign revocation audit scope")
			}
		}
		if audit := event.CredentialRevocation; audit != nil {
			principal, principalErr := accessidentity.Parse(audit.Principal)
			credential, credentialErr := accessstate.ParseCredentialID(audit.Credential)
			slug, scopeErr := routing.ParseWorkspaceSlug(audit.Workspace)
			if principalErr != nil || !principal.Managed() || credentialErr != nil || credential.String() != audit.Credential || scopeErr != nil || slug.String() != audit.Workspace || audit.Workspace != event.Workspace || !validManagementActor(audit.Caller) || event.Actor != audit.Caller || event.Action != "credential.revoke" || audit.Revision == "" || audit.CreatedAt.IsZero() || !audit.CommittedAt.Equal(event.CommittedAt) {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid credential revocation audit")
			}
		}
		if audit := event.CredentialIssuance; audit != nil {
			principal, principalErr := accessidentity.Parse(audit.Principal)
			credential, credentialErr := accessstate.ParseCredentialID(audit.Credential)
			slug, scopeErr := routing.ParseWorkspaceSlug(audit.Workspace)
			if principalErr != nil || !principal.Managed() || credentialErr != nil || credential.String() != audit.Credential || scopeErr != nil || slug.String() != audit.Workspace || audit.Workspace != event.Workspace || audit.Caller != "local" || event.Actor != audit.Caller || event.Action != "credential.issue" || audit.CreatedAt.IsZero() || !audit.CommittedAt.Equal(event.CommittedAt) {
				return managementaudit.Snapshot{}, fmt.Errorf("operator client: invalid credential issuance audit")
			}
		}
	}
	return result, nil
}

func decodeAuditFactFields(raw json.RawMessage, count int) (map[string]json.RawMessage, error) {
	invalid := fmt.Errorf("operator client: invalid audit facts")
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, invalid
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		member, err := decoder.Token()
		if err != nil {
			return nil, invalid
		}
		name, ok := member.(string)
		if _, exists := fields[name]; !ok || exists {
			return nil, invalid
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, invalid
		}
		fields[name] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || decoder.Decode(&struct{}{}) != io.EOF || len(fields) != count {
		return nil, invalid
	}
	return fields, nil
}

// Management actors are admitted local or managed identities. This validates
// readback shape only; it cannot grant Node authority to the reader.
func validManagementActor(raw string) bool { _, err := accessidentity.Parse(raw); return err == nil }
