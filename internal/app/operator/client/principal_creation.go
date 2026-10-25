package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
)

// CreatePrincipal sends exactly one identity-only request. Caller owns consent
// and retains label/key for explicit recovery; transport replay is disabled.
func (c *Client) CreatePrincipal(ctx context.Context, label, key string) (accessapp.Principal, bool, error) {
	if label == "" || len(label) > 128 || !utf8.ValidString(label) || strings.TrimSpace(label) != label || len(key) == 0 || len(key) > 256 {
		return accessapp.Principal{}, false, errors.New("exact label and creation retry key required")
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return accessapp.Principal{}, false, errors.New("label contains control characters")
		}
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return accessapp.Principal{}, false, errors.New("invalid creation retry key")
		}
	}
	raw, err := json.Marshal(map[string]string{"label": label})
	if err != nil {
		return accessapp.Principal{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/principals", bytes.NewReader(raw))
	if err != nil {
		return accessapp.Principal{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	req.GetBody = nil
	response, err := c.http.Do(req)
	if err != nil {
		return accessapp.Principal{}, false, errors.New("Principal creation not acknowledged; outcome may have committed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 201 {
		return accessapp.Principal{}, false, errorFromResponse(response, "Principal creation failed")
	}
	var principal accessapp.Principal
	decoder := json.NewDecoder(response.Body)
	var wire map[string]json.RawMessage
	if err := decoder.Decode(&wire); err != nil || wire == nil {
		return accessapp.Principal{}, false, errors.New("creation acknowledgement invalid; outcome may have committed")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return accessapp.Principal{}, false, errors.New("creation acknowledgement has trailing data; outcome may have committed")
	}
	// Known secret-bearing projections cannot be laundered into an identity-only
	// acknowledgement by ignoring additive fields. Never include their values in
	// errors or return partial decoded metadata on a contradictory response.
	for _, forbidden := range []string{"token", "secret", "digest", "credentials"} {
		if _, exists := wire[forbidden]; exists {
			return accessapp.Principal{}, false, errors.New("creation acknowledgement contains non-identity material; inspect metadata before recovery")
		}
	}
	rawPrincipal, err := json.Marshal(wire)
	if err != nil || json.Unmarshal(rawPrincipal, &principal) != nil || len(wire["permissions"]) == 0 || string(wire["permissions"]) == "null" {
		return accessapp.Principal{}, false, errors.New("creation acknowledgement missing valid identity metadata; outcome may have committed")
	}
	if response.StatusCode == http.StatusCreated && (principal.Status != "active" || len(principal.Permissions) != 0) {
		return accessapp.Principal{}, false, errors.New("fresh creation acknowledgement contradicts identity-only effect; inspect metadata before recovery")
	}
	if _, err := accessidentity.RestoreManaged(principal.ID); err != nil || principal.Label != label || principal.Kind != "managed" || (principal.Status != "active" && principal.Status != "revoked") {
		return accessapp.Principal{}, false, errors.New("creation acknowledgement contradicts retained intent")
	}
	return principal, response.StatusCode == 200, nil
}
