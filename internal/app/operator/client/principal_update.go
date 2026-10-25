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
	"unicode"
	"unicode/utf8"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
)

// UpdatePrincipalLabel sends the exact reviewed revision once. Caller owns
// trusted consent; this method neither refreshes consent nor retries an uncertain
// write. Inspect current metadata before deciding how to recover a lost response.
func (c *Client) UpdatePrincipalLabel(ctx context.Context, id, label, revision string) (accessapp.Principal, error) {
	if _, err := accessidentity.RestoreManaged(id); err != nil {
		return accessapp.Principal{}, errors.New("managed Principal required")
	}
	if label == "" || len(label) > 128 || !utf8.ValidString(label) || strings.TrimSpace(label) != label {
		return accessapp.Principal{}, errors.New("exact valid Principal label required")
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return accessapp.Principal{}, errors.New("Principal label contains control characters")
		}
	}
	if revision == "" || strings.ContainsAny(revision, "\"\\, \t\r\n") {
		return accessapp.Principal{}, errors.New("reviewed Principal revision required")
	}
	raw, err := json.Marshal(map[string]string{"label": label})
	if err != nil {
		return accessapp.Principal{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+"/api/v1/principals/"+url.PathEscape(id), bytes.NewReader(raw))
	if err != nil {
		return accessapp.Principal{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+revision+`"`)
	req.GetBody = nil
	response, err := c.http.Do(req)
	if err != nil {
		return accessapp.Principal{}, errors.New("Principal label update not acknowledged; outcome may have committed; inspect current metadata")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return accessapp.Principal{}, errorFromResponse(response, "Principal label update failed")
	}
	var wire map[string]json.RawMessage
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&wire); err != nil || wire == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return accessapp.Principal{}, errors.New("invalid label update acknowledgement; outcome may have committed")
	}
	for _, field := range []string{"token", "secret", "digest", "credentials"} {
		if _, exists := wire[field]; exists {
			return accessapp.Principal{}, errors.New("label update acknowledgement contains non-identity material")
		}
	}
	var principal accessapp.Principal
	encoded, err := json.Marshal(wire)
	if err != nil || json.Unmarshal(encoded, &principal) != nil || len(wire["permissions"]) == 0 || string(wire["permissions"]) == "null" || principal.ID != id || principal.Label != label || principal.Kind != "managed" || (principal.Status != "active" && principal.Status != "revoked") || principal.Revision == "" || response.Header.Get("ETag") != `"`+principal.Revision+`"` {
		return accessapp.Principal{}, errors.New("label update acknowledgement contradicts reviewed identity; inspect current metadata")
	}
	return principal, nil
}
