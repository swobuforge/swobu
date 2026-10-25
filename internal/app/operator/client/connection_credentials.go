package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// ConnectionCredentialReplacement is the redacted acknowledgement returned
// after replacing a Connection-owned credential binding.
type ConnectionCredentialReplacement struct {
	Connection map[string]json.RawMessage `json:"connection"`
	Replayed   bool                       `json:"replayed"`
}

// ReplaceConnectionCredential sends one reviewed, idempotent credential
// replacement through the canonical Connection API. The secret is never
// included in an acknowledgement or error surface.
func (c *Client) ReplaceConnectionCredential(ctx context.Context, id, revision, key, secret, environment string) (ConnectionCredentialReplacement, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(revision) == "" || strings.TrimSpace(key) == "" || strings.TrimSpace(secret) == "" {
		return ConnectionCredentialReplacement{}, errors.New("operator client: Connection ID, revision, idempotency key and secret are required")
	}
	body, err := json.Marshal(struct {
		Secret       string `json:"secret"`
		Environment  string `json:"environment,omitempty"`
		SameIdentity bool   `json:"same_identity"`
	}{Secret: secret, Environment: environment, SameIdentity: true})
	if err != nil {
		return ConnectionCredentialReplacement{}, errors.New("operator client: Connection credential payload could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/connections/"+url.PathEscape(id)+"/credential-replacements", bytes.NewReader(body))
	if err != nil {
		return ConnectionCredentialReplacement{}, errors.New("operator client: Connection credential request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+revision+`"`)
	req.Header.Set("Idempotency-Key", key)
	resp, err := c.http.Do(req)
	if err != nil {
		return ConnectionCredentialReplacement{}, errors.New("operator client: Connection credential replacement outcome is unknown")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ConnectionCredentialReplacement{}, errorFromResponse(resp, "operator client: Connection credential replacement failed")
	}
	var result ConnectionCredentialReplacement
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.Connection == nil {
		return ConnectionCredentialReplacement{}, errors.New("operator client: invalid Connection credential replacement acknowledgement")
	}
	for _, field := range []string{"secret", "credential_ref", "token"} {
		if _, exists := result.Connection[field]; exists {
			return ConnectionCredentialReplacement{}, errors.New("operator client: credential replacement acknowledgement contains secret material")
		}
	}
	return result, nil
}
