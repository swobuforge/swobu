package operatorclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
)

// PrincipalRevocationIntent retains original consent scope for explicit recovery.
// Construction is neither human approval nor management admission.
type PrincipalRevocationIntent struct{ id, revision, key string }

// RestorePrincipalRevocationIntent validates original values without refreshing
// revision or treating receipt possession as approval.
func RestorePrincipalRevocationIntent(id, revision, key string) (PrincipalRevocationIntent, error) {
	if _, err := accessidentity.RestoreManaged(id); err != nil {
		return PrincipalRevocationIntent{}, err
	}
	if revision == "" || key == "" || len(key) > 256 {
		return PrincipalRevocationIntent{}, errors.New("exact Principal revision and retry key required")
	}
	for _, value := range []string{revision, key} {
		for _, r := range value {
			if r < 33 || r > 126 || r == '"' || r == '\\' || r == ',' {
				return PrincipalRevocationIntent{}, errors.New("unsafe original revocation intent")
			}
		}
	}
	return PrincipalRevocationIntent{id: id, revision: revision, key: key}, nil
}

// PreviewPrincipalRevocation requests server-owned impact without reserving a key.
func (c *Client) PreviewPrincipalRevocation(ctx context.Context, id, revision string) (accessapp.PrincipalRevocation, error) {
	intent, err := RestorePrincipalRevocationIntent(id, revision, "preview-only")
	if err != nil {
		return accessapp.PrincipalRevocation{}, err
	}
	return c.principalRevocation(ctx, intent, true)
}

// ApplyPrincipalRevocation sends immutable intent once; caller owns fresh trusted
// confirmation. Transport replay and automatic consent refresh are disabled.
func (c *Client) ApplyPrincipalRevocation(ctx context.Context, intent PrincipalRevocationIntent) (accessapp.PrincipalRevocation, error) {
	if _, err := RestorePrincipalRevocationIntent(intent.id, intent.revision, intent.key); err != nil {
		return accessapp.PrincipalRevocation{}, err
	}
	return c.principalRevocation(ctx, intent, false)
}

func (c *Client) principalRevocation(ctx context.Context, intent PrincipalRevocationIntent, preview bool) (accessapp.PrincipalRevocation, error) {
	path := c.baseURL + "/api/v1/principals/" + url.PathEscape(intent.id)
	if preview {
		path += "?dry_run=true"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, path, strings.NewReader(`{}`))
	if err != nil {
		return accessapp.PrincipalRevocation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+intent.revision+`"`)
	if !preview {
		req.Header.Set("Idempotency-Key", intent.key)
	}
	req.GetBody = nil
	response, err := c.http.Do(req)
	if err != nil {
		return accessapp.PrincipalRevocation{}, errors.New("Principal revocation not acknowledged; retain original intent and inspect before recovery")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return accessapp.PrincipalRevocation{}, errorFromResponse(response, "Principal revocation failed")
	}
	var wire struct {
		Principal json.RawMessage `json:"principal"`
		Count     *int            `json:"credential_count"`
		Replayed  *bool           `json:"replayed"`
	}
	decoder := json.NewDecoder(response.Body)
	var envelope map[string]json.RawMessage
	if decoder.Decode(&envelope) != nil || envelope == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return accessapp.PrincipalRevocation{}, errors.New("incomplete revocation acknowledgement; inspect current metadata")
	}
	for _, name := range []string{"token", "secret", "digest", "credentials", "revocation_receipts"} {
		if _, exists := envelope[name]; exists {
			return accessapp.PrincipalRevocation{}, errors.New("revocation response contains non-identity material")
		}
	}
	raw, err := json.Marshal(envelope)
	if err != nil || json.Unmarshal(raw, &wire) != nil || wire.Count == nil || *wire.Count < 0 || wire.Replayed == nil {
		return accessapp.PrincipalRevocation{}, errors.New("incomplete revocation acknowledgement; inspect current metadata")
	}
	var fields map[string]json.RawMessage
	var principal accessapp.Principal
	if json.Unmarshal(wire.Principal, &fields) != nil || fields == nil {
		return accessapp.PrincipalRevocation{}, errors.New("invalid revocation identity")
	}
	for _, name := range []string{"token", "secret", "digest", "credentials", "revocation_receipts"} {
		if _, exists := fields[name]; exists {
			return accessapp.PrincipalRevocation{}, errors.New("revocation response contains non-identity material")
		}
	}
	if json.Unmarshal(wire.Principal, &principal) != nil || principal.ID != intent.id || principal.Kind != "managed" || principal.Revision == "" || len(fields["permissions"]) == 0 || string(fields["permissions"]) == "null" || response.Header.Get("ETag") != `"`+principal.Revision+`"` {
		return accessapp.PrincipalRevocation{}, errors.New("revocation response contradicts retained identity")
	}
	if preview {
		if *wire.Replayed || principal.Revision != intent.revision || (principal.Status != "active" && principal.Status != "revoked") {
			return accessapp.PrincipalRevocation{}, errors.New("preview contradicts reviewed revision")
		}
	} else if principal.Status != "revoked" || len(principal.Permissions) != 0 || *wire.Count != 0 {
		return accessapp.PrincipalRevocation{}, errors.New("revocation acknowledgement retains authority")
	}
	return accessapp.PrincipalRevocation{Principal: principal, CredentialCount: *wire.Count, Replayed: *wire.Replayed}, nil
}
