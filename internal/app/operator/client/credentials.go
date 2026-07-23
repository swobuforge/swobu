package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// StorePastedCredential asks the daemon to persist transient credential
// material under a stable slot and returns its runtime-resolvable reference.
func (c *Client) StorePastedCredential(ctx context.Context, providerSpec, name, secret string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"provider_spec": strings.TrimSpace(providerSpec), // swobu:io-string source=boundary
		"name":          strings.TrimSpace(name),         // swobu:io-string source=boundary
		"secret":        strings.TrimSpace(secret),       // swobu:io-string source=boundary
	})
	if err != nil {
		return "", fmt.Errorf("operator client: credential store payload could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/_swobu/credentials", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("operator client: credential store request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("operator client: credential store is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errorFromResponse(resp, "operator client: credential store failed")
	}
	var result struct {
		CredentialRef string `json:"credential_ref"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("operator client: credential store response could not be decoded")
	}
	ref := strings.TrimSpace(result.CredentialRef) // swobu:io-string source=boundary
	if ref == "" {
		return "", fmt.Errorf("operator client: credential store returned an empty reference")
	}
	return ref, nil
}
