package operatorclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
)

func (c *Client) IssueAccessCredential(ctx context.Context, accessID, workspace string) (accessapp.IssueResult, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return accessapp.IssueResult{}, fmt.Errorf("operator client: issuance retry key could not be generated")
	}
	intent, err := RestoreCredentialIssuanceIntent(accessID, workspace, hex.EncodeToString(key[:]))
	if err != nil {
		return accessapp.IssueResult{}, err
	}
	out, err := c.ApplyCredentialIssuance(ctx, intent)
	if err != nil {
		return accessapp.IssueResult{}, fmt.Errorf("%w; retain original Principal %s, Workspace %s and retry key %s for separately confirmed recovery", err, accessID, workspace, intent.key)
	}
	return out, nil
}

// ListPrincipals returns the versioned ontology projection. It is available
// only from the Node Control client; the legacy client has no such route.
func (c *Client) ListPrincipals(ctx context.Context) ([]accessapp.Principal, error) {
	var out []accessapp.Principal
	err := c.accessRequest(ctx, http.MethodGet, c.operatorPath("/principals"), nil, http.StatusOK, &out)
	return out, err
}

// GetPrincipal inspects canonical identity metadata, including revoked identity
// attribution. It never falls back to the legacy credential-bearing collection.
func (c *Client) GetPrincipal(ctx context.Context, id string) (accessapp.Principal, error) {
	var out accessapp.Principal
	err := c.accessRequest(ctx, http.MethodGet, "/api/v1/principals/"+url.PathEscape(id), nil, http.StatusOK, &out)
	return out, err
}

// ListCredentials reads canonical metadata without recovering a one-shot secret
// or issuing a replacement. Missing canonical endpoints remain failures.
func (c *Client) ListCredentials(ctx context.Context, id string) ([]accessapp.Credential, error) {
	var out []accessapp.Credential
	err := c.accessRequest(ctx, http.MethodGet, "/api/v1/principals/"+url.PathEscape(id)+"/credentials", nil, http.StatusOK, &out)
	return out, err
}
func (c *Client) accessRequest(ctx context.Context, method, path string, body any, want int, out any) error {
	return c.accessRequestWithHeaders(ctx, method, path, body, want, out, nil)
}
func (c *Client) accessRequestWithHeaders(ctx context.Context, method, path string, body any, want int, out any, headers map[string]string) error {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("operator client: Access payload could not be encoded")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("operator client: Access request could not be built")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		if value != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("operator client: Access service is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return errorFromResponse(resp, "operator client: Access command failed")
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("operator client: Access response could not be decoded")
		}
	}
	return nil
}
