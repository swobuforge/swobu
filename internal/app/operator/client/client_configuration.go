package operatorclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/swobuforge/swobu/internal/accessstate"
	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// PreviewClientConfiguration reads an effect-free local review. Client-owned
// before-values are private operator data, not a model-safe diagnostic payload.
func (client *Client) PreviewClientConfiguration(ctx context.Context, workspace, clientID, principal, modelID string) (connectapp.Plan, error) {
	if _, err := accessidentity.RestoreManaged(principal); err != nil {
		return connectapp.Plan{}, errors.New("existing managed Principal required")
	}
	slug, err := routing.ParseWorkspaceSlug(workspace)
	if err != nil || slug.String() != workspace {
		return connectapp.Plan{}, errors.New("exact Workspace slug required")
	}
	if _, err := clientconnect.ParseClientID(clientID); err != nil {
		return connectapp.Plan{}, err
	}
	if modelID == "" {
		return connectapp.Plan{}, errors.New("explicit model ID required")
	}
	var acknowledgement struct {
		connectapp.Plan
		AlreadyConfigured *bool `json:"already_configured"`
		RequiresReplace   *bool `json:"requires_replace"`
		Changes           []struct {
			Field        *string `json:"field"`
			Before       *string `json:"before"`
			BeforeExists *bool   `json:"before_exists"`
			After        *string `json:"after"`
		} `json:"changes"`
	}
	body := struct {
		Workspace string `json:"workspace"`
		ModelID   string `json:"model_id"`
		Client    string `json:"client"`
		Principal string `json:"principal"`
	}{workspace, modelID, clientID, principal}
	if err := client.workspaceRequest(ctx, http.MethodPost, "/api/v1/client-configurations?dry_run=true", body, http.StatusOK, &acknowledgement); err != nil {
		return connectapp.Plan{}, err
	}
	plan := acknowledgement.Plan
	if plan.Workspace != workspace || plan.ModelID != modelID || plan.Client.ID != clientID || plan.Authentication.Principal != principal || !validClientAuthentication(plan.Authentication) || plan.ConfigPaths == nil || acknowledgement.Changes == nil {
		return connectapp.Plan{}, errors.New("invalid client configuration preview acknowledgement")
	}
	if acknowledgement.AlreadyConfigured == nil || acknowledgement.RequiresReplace == nil {
		return connectapp.Plan{}, errors.New("missing client configuration preview decisions")
	}
	plan.Changes = make([]connectapp.Change, 0, len(acknowledgement.Changes))
	changes := make([]clientconnect.Change, 0, len(acknowledgement.Changes))
	for _, change := range acknowledgement.Changes {
		if change.Field == nil || *change.Field == "" || change.Before == nil || change.BeforeExists == nil || change.After == nil {
			return connectapp.Plan{}, errors.New("incomplete client configuration preview change")
		}
		plan.Changes = append(plan.Changes, connectapp.Change{Field: *change.Field, Before: *change.Before, BeforeExists: *change.BeforeExists, After: *change.After})
		changes = append(changes, clientconnect.Change{Field: *change.Field, Before: *change.Before, BeforeExists: *change.BeforeExists, After: *change.After})
	}
	review := clientconnect.Plan{Changes: changes}
	if *acknowledgement.AlreadyConfigured != review.AlreadyConfigured() || *acknowledgement.RequiresReplace != review.RequiresReplace() {
		return connectapp.Plan{}, errors.New("contradictory client configuration preview decisions")
	}
	plan.AlreadyConfigured = *acknowledgement.AlreadyConfigured
	plan.RequiresReplace = *acknowledgement.RequiresReplace
	return plan, nil
}

func validClientAuthentication(auth connectapp.Authentication) bool {
	if _, err := accessidentity.RestoreManaged(auth.Principal); err != nil || auth.PermissionRevision == "" {
		return false
	}
	if auth.IssueCredential {
		return auth.CredentialID == ""
	}
	_, err := accessstate.ParseCredentialID(auth.CredentialID)
	return err == nil
}
