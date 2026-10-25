package operatorclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"

	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/routing"
)

// ClientConfigurationIntent privately freezes the original reviewed request and
// retry key. It carries private before-values, not admission or human approval.
type ClientConfigurationIntent struct {
	body, workspace, modelID, client, principal, revision, key string
	paths                                                      []string
}

// NewClientConfigurationIntent retains a review without I/O or consent refresh.
// Caller-owned trusted confirmation is required before apply or explicit recovery.
func NewClientConfigurationIntent(plan connectapp.Plan, key string) (ClientConfigurationIntent, error) {
	if !validClientAuthentication(plan.Authentication) || plan.Authentication.IssueCredential && plan.AlreadyConfigured {
		return ClientConfigurationIntent{}, errors.New("complete authenticated Connect review required")
	}
	slug, err := routing.ParseWorkspaceSlug(plan.Workspace)
	if err != nil || slug.String() != plan.Workspace {
		return ClientConfigurationIntent{}, errors.New("exact Workspace slug required")
	}
	if _, err := clientconnect.ParseClientID(plan.Client.ID); err != nil {
		return ClientConfigurationIntent{}, err
	}
	if plan.ModelID == "" || !validConnectRevision(plan.Revision) || len(key) < 16 || len(key) > 256 || strings.ContainsAny(key, "\r\n") || plan.ConfigPaths == nil || plan.Changes == nil {
		return ClientConfigurationIntent{}, errors.New("original Connect review and retry key required")
	}
	changes := make([]clientconnect.Change, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		if change.Field == "" {
			return ClientConfigurationIntent{}, errors.New("incomplete Connect change")
		}
		changes = append(changes, clientconnect.Change{Field: change.Field, Before: change.Before, BeforeExists: change.BeforeExists, After: change.After})
	}
	review := clientconnect.Plan{Changes: changes}
	if review.AlreadyConfigured() != plan.AlreadyConfigured || review.RequiresReplace() != plan.RequiresReplace {
		return ClientConfigurationIntent{}, errors.New("contradictory Connect review")
	}
	body, err := json.Marshal(struct {
		Workspace string            `json:"workspace"`
		ModelID   string            `json:"model_id"`
		Client    string            `json:"client"`
		Principal string            `json:"principal"`
		Review    connectapp.Review `json:"review"`
	}{plan.Workspace, plan.ModelID, plan.Client.ID, plan.Authentication.Principal, plan.Review()})
	if err != nil {
		return ClientConfigurationIntent{}, errors.New("Connect intent could not be retained")
	}
	return ClientConfigurationIntent{body: string(body), workspace: plan.Workspace, modelID: plan.ModelID, client: plan.Client.ID, principal: plan.Authentication.Principal, revision: plan.Revision, key: key, paths: slices.Clone(plan.ConfigPaths)}, nil
}

func validConnectRevision(revision string) bool {
	if !strings.HasPrefix(revision, "connect-") || len(revision) != len("connect-")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(revision, "connect-"))
	return err == nil
}

// ApplyClientConfiguration sends retained intent once. Recovery uses the same
// intent only after caller-owned confirmation; uncertainty never triggers retry.
// Replayed results describe historical completion, not current file contents.
func (client *Client) ApplyClientConfiguration(ctx context.Context, intent ClientConfigurationIntent) (connectapp.ApplyResult, error) {
	if intent.body == "" {
		return connectapp.ApplyResult{}, errors.New("retained Connect intent required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/api/v1/client-configurations?dry_run=false", strings.NewReader(intent.body))
	if err != nil {
		return connectapp.ApplyResult{}, errors.New("Connect request could not be built")
	}
	request.GetBody = nil
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"`+intent.revision+`"`)
	request.Header.Set("Idempotency-Key", intent.key)
	response, err := client.http.Do(request)
	if err != nil {
		return connectapp.ApplyResult{}, errors.New("Connect not acknowledged; retain original intent and inspect before recovery")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return connectapp.ApplyResult{}, errorFromResponse(response, "Connect apply failed")
	}
	var wire struct {
		connectapp.Plan
		AlreadyConfigured *bool `json:"already_configured"`
		RequiresReplace   *bool `json:"requires_replace"`
		Replayed          *bool `json:"replayed"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20+1))
	if decoder.Decode(&wire) != nil || decoder.Decode(&struct{}{}) != io.EOF || wire.Workspace != intent.workspace || wire.ModelID != intent.modelID || wire.Client.ID != intent.client || wire.ConfigPaths == nil || !slices.Equal(wire.ConfigPaths, intent.paths) || wire.Changes == nil || len(wire.Changes) != 0 || wire.AlreadyConfigured == nil || !*wire.AlreadyConfigured || wire.RequiresReplace == nil || *wire.RequiresReplace || wire.Replayed == nil || !validConnectRevision(wire.Revision) || response.Header.Get("ETag") != `"`+wire.Revision+`"` {
		return connectapp.ApplyResult{}, errors.New("invalid Connect acknowledgement; retain original intent and inspect before recovery")
	}
	if !validClientAuthentication(wire.Authentication) || wire.Authentication.Principal != intent.principal || wire.Authentication.IssueCredential {
		return connectapp.ApplyResult{}, errors.New("Connect authentication acknowledgement does not match original intent")
	}
	wire.Plan.AlreadyConfigured = true
	wire.Plan.RequiresReplace = false
	return connectapp.ApplyResult{Plan: wire.Plan, Replayed: *wire.Replayed}, nil
}
