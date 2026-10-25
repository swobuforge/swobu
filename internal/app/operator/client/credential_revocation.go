package operatorclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/accessstate"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/routing"
)

// CredentialRevocationIntent privately retains original selected scope for
// explicit recovery. Construction is neither admission nor human confirmation.
type CredentialRevocationIntent struct{ principal, credential, revision, key string }

// RestoreCredentialRevocationIntent validates original consent without refreshing
// it from current metadata or treating a retry key as approval.
func RestoreCredentialRevocationIntent(principal, credential, revision, key string) (CredentialRevocationIntent, error) {
	if _, err := RestorePrincipalRevocationIntent(principal, revision, key); err != nil {
		return CredentialRevocationIntent{}, err
	}
	if _, err := accessstate.ParseCredentialID(credential); err != nil {
		return CredentialRevocationIntent{}, err
	}
	return CredentialRevocationIntent{principal, credential, revision, key}, nil
}

func (c *Client) credentialRevocationRequest(ctx context.Context, principal, credential string, preview bool) (*http.Request, error) {
	path := c.baseURL + "/api/v1/principals/" + url.PathEscape(principal) + "/credentials/" + url.PathEscape(credential)
	if preview {
		path += "?dry_run=true"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, path, strings.NewReader(`{}`))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.GetBody = nil
	}
	return req, err
}

// PreviewCredentialRevocation requests server-owned selected impact without
// consent headers, effects or key reservation. It sends once and returns no secret.
func (c *Client) PreviewCredentialRevocation(ctx context.Context, principal, credential string) (accessapp.CredentialRevocationPreview, error) {
	if _, err := RestoreCredentialRevocationIntent(principal, credential, "validation-only", "validation-only"); err != nil {
		return accessapp.CredentialRevocationPreview{}, err
	}
	req, err := c.credentialRevocationRequest(ctx, principal, credential, true)
	if err != nil {
		return accessapp.CredentialRevocationPreview{}, err
	}
	response, err := c.http.Do(req)
	if err != nil {
		return accessapp.CredentialRevocationPreview{}, errors.New("credential preview unavailable; no revocation sent")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return accessapp.CredentialRevocationPreview{}, errorFromResponse(response, "credential preview failed")
	}
	envelope, err := decodeCredentialRevocationEnvelope(response.Body)
	if err != nil {
		return accessapp.CredentialRevocationPreview{}, err
	}
	raw, _ := json.Marshal(envelope)
	var impact accessapp.CredentialRevocationPreview
	if json.Unmarshal(raw, &impact) != nil || impact.Principal != principal || impact.Credential.ID != credential || impact.Credential.CreatedAt.IsZero() || impact.ApprovalClass != "trusted_confirmation" || impact.Warning == "" || response.Header.Get("ETag") != `"`+impact.Revision+`"` {
		return accessapp.CredentialRevocationPreview{}, errors.New("credential preview contradicts selected impact")
	}
	if _, err := routing.ParseWorkspaceSlug(impact.Credential.Workspace); err != nil {
		return accessapp.CredentialRevocationPreview{}, errors.New("invalid credential preview audience")
	}
	if _, err := RestoreCredentialRevocationIntent(principal, credential, impact.Revision, "validation-only"); err != nil {
		return accessapp.CredentialRevocationPreview{}, err
	}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(envelope["credential"], &metadata) != nil || credentialRevocationHasPrivateFields(metadata) {
		return accessapp.CredentialRevocationPreview{}, errors.New("invalid credential preview metadata")
	}
	return impact, nil
}

// ApplyCredentialRevocation sends original intent once after caller-owned trusted
// confirmation. Uncertainty never refreshes consent or retries automatically.
func (c *Client) ApplyCredentialRevocation(ctx context.Context, intent CredentialRevocationIntent) (accessapp.CredentialRevocation, error) {
	if _, err := RestoreCredentialRevocationIntent(intent.principal, intent.credential, intent.revision, intent.key); err != nil {
		return accessapp.CredentialRevocation{}, err
	}
	req, err := c.credentialRevocationRequest(ctx, intent.principal, intent.credential, false)
	if err != nil {
		return accessapp.CredentialRevocation{}, err
	}
	req.Header.Set("If-Match", `"`+intent.revision+`"`)
	req.Header.Set("Idempotency-Key", intent.key)
	response, err := c.http.Do(req)
	if err != nil {
		return accessapp.CredentialRevocation{}, errors.New("credential revocation not acknowledged; retain original intent and inspect before recovery")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return accessapp.CredentialRevocation{}, errorFromResponse(response, "credential revocation failed")
	}
	envelope, err := decodeCredentialRevocationEnvelope(response.Body)
	if err != nil {
		return accessapp.CredentialRevocation{}, err
	}
	raw, _ := json.Marshal(envelope)
	var wire struct {
		Principal  json.RawMessage `json:"principal"`
		Credential string          `json:"credential"`
		Revoked    *bool           `json:"revoked"`
		Replayed   *bool           `json:"replayed"`
	}
	if json.Unmarshal(raw, &wire) != nil || wire.Credential != intent.credential || wire.Revoked == nil || !*wire.Revoked || wire.Replayed == nil {
		return accessapp.CredentialRevocation{}, errors.New("incomplete credential revocation acknowledgement; inspect current metadata")
	}
	var fields map[string]json.RawMessage
	var principal accessapp.Principal
	if json.Unmarshal(wire.Principal, &fields) != nil || fields == nil || credentialRevocationHasPrivateFields(fields) || json.Unmarshal(wire.Principal, &principal) != nil || principal.ID != intent.principal || principal.Kind != "managed" || principal.Revision == "" || (principal.Status != "active" && principal.Status != "revoked") || len(fields["permissions"]) == 0 || string(fields["permissions"]) == "null" || response.Header.Get("ETag") != `"`+principal.Revision+`"` || (principal.Status == "revoked" && len(principal.Permissions) != 0) {
		return accessapp.CredentialRevocation{}, errors.New("credential revocation response contradicts current identity")
	}
	return accessapp.CredentialRevocation{Principal: principal, Credential: wire.Credential, Revoked: true, Replayed: *wire.Replayed}, nil
}

func credentialRevocationHasPrivateFields(fields map[string]json.RawMessage) bool {
	for _, name := range []string{"token", "secret", "digest", "credentials", "revocation_receipts", "credential_revocation_receipts"} {
		if _, exists := fields[name]; exists {
			return true
		}
	}
	return false
}

func decodeCredentialRevocationEnvelope(body io.Reader) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(body)
	var envelope map[string]json.RawMessage
	if decoder.Decode(&envelope) != nil || envelope == nil || decoder.Decode(&struct{}{}) != io.EOF || credentialRevocationHasPrivateFields(envelope) {
		return nil, errors.New("invalid credential revocation acknowledgement; inspect current metadata")
	}
	return envelope, nil
}
