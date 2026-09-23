package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
)

func (c *Client) ListAccess(ctx context.Context) ([]accessapp.Access, error) {
	var out []accessapp.Access
	err := c.accessRequest(ctx, http.MethodGet, "/_swobu/access", nil, http.StatusOK, &out)
	return out, err
}
func (c *Client) CreateAccess(ctx context.Context, name, workspace string) (accessapp.IssueResult, error) {
	var out accessapp.IssueResult
	err := c.accessRequest(ctx, http.MethodPost, "/_swobu/access", map[string]string{"name": name, "workspace": workspace}, http.StatusCreated, &out)
	return out, err
}
func (c *Client) IssueAccessCredential(ctx context.Context, accessID, workspace string) (accessapp.IssueResult, error) {
	var out accessapp.IssueResult
	err := c.accessRequest(ctx, http.MethodPost, "/_swobu/access/"+url.PathEscape(accessID)+"/credentials", map[string]string{"workspace": workspace}, http.StatusCreated, &out)
	return out, err
}
func (c *Client) RevokeAccessCredential(ctx context.Context, accessID, credentialID string) error {
	return c.accessRequest(ctx, http.MethodDelete, "/_swobu/access/"+url.PathEscape(accessID)+"/credentials/"+url.PathEscape(credentialID), struct{}{}, http.StatusNoContent, nil)
}
func (c *Client) RevokeAccess(ctx context.Context, accessID string) error {
	return c.accessRequest(ctx, http.MethodPost, "/_swobu/access/"+url.PathEscape(accessID)+"/revoke", struct{}{}, http.StatusNoContent, nil)
}
func (c *Client) accessRequest(ctx context.Context, method, path string, body any, want int, out any) error {
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
