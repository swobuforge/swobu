package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// CredentialIssuanceIntent is immutable exact issuance scope and retry identity.
// It is neither admission nor approval. Trusted consumers disclose it and obtain
// fresh confirmation, including recovery which may perform the first issuance.
type CredentialIssuanceIntent struct {
	principal string
	workspace string
	key       string
}

// RestoreCredentialIssuanceIntent validates original scope and key without
// refreshing authority, inventing a new key, or implying human approval.
func RestoreCredentialIssuanceIntent(principal, workspace, key string) (CredentialIssuanceIntent, error) {
	if _, err := accessidentity.RestoreManaged(principal); err != nil {
		return CredentialIssuanceIntent{}, err
	}
	if _, err := routing.ParseWorkspaceSlug(workspace); err != nil {
		return CredentialIssuanceIntent{}, err
	}
	if len(key) == 0 || len(key) > 256 || strings.ContainsAny(key, " \t\r\n") {
		return CredentialIssuanceIntent{}, errors.New("invalid issuance retry key")
	}
	return CredentialIssuanceIntent{principal: principal, workspace: workspace, key: key}, nil
}

// ApplyCredentialIssuance sends once; uncertainty never causes automatic retry.
// A matching committed receipt returns metadata, not successful secret delivery.
func (c *Client) ApplyCredentialIssuance(ctx context.Context, intent CredentialIssuanceIntent) (accessapp.IssueResult, error) {
	if _, err := RestoreCredentialIssuanceIntent(intent.principal, intent.workspace, intent.key); err != nil {
		return accessapp.IssueResult{}, err
	}
	raw, _ := json.Marshal(map[string]string{"workspace": intent.workspace})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/principals/"+url.PathEscape(intent.principal)+"/credentials", bytes.NewReader(raw))
	if err != nil {
		return accessapp.IssueResult{}, errors.New("issuance request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", intent.key)
	// net/http treats Idempotency-Key as replay permission on a reused broken
	// connection. This one-shot operation requires fresh human recovery consent;
	// a rewindable body must not silently authorize transport-level replay.
	req.GetBody = nil
	resp, err := c.http.Do(req)
	if err != nil {
		return accessapp.IssueResult{}, errors.New("credential issuance was not acknowledged; outcome may have committed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return accessapp.IssueResult{}, errorFromResponse(resp, "credential issuance failed")
	}
	var result accessapp.IssueResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return accessapp.IssueResult{}, errors.New("issuance acknowledgement invalid; outcome may have committed")
	}
	if result.Access.ID != intent.principal || result.Credential.ID == "" || result.Credential.Workspace != intent.workspace || (resp.StatusCode == http.StatusCreated && (result.Token == "" || result.SecretNotRecoverable)) || (resp.StatusCode == http.StatusOK && (result.Token != "" || !result.SecretNotRecoverable)) {
		return accessapp.IssueResult{}, fmt.Errorf("issuance acknowledgement contradicts original scope or one-shot outcome; inspect metadata before recovery")
	}
	return result, nil
}
