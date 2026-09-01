package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/swobuforge/swobu/internal/app/operator/shares"
	"github.com/swobuforge/swobu/internal/sharestate"
)

func (c *Client) ListShares(ctx context.Context) ([]shares.Summary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_swobu/shares", nil)
	if err != nil {
		return nil, fmt.Errorf("operator client: share list request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("operator client: share service is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errorFromResponse(resp, "operator client: share list failed")
	}
	var summaries []shares.Summary
	if err := json.NewDecoder(resp.Body).Decode(&summaries); err != nil {
		return nil, fmt.Errorf("operator client: share list response could not be decoded")
	}
	return summaries, nil
}

func (c *Client) RevealShare(ctx context.Context, route string) (shares.Result, error) {
	query := url.Values{"route": []string{route}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_swobu/shares?"+query.Encode(), nil)
	if err != nil {
		return shares.Result{}, fmt.Errorf("operator client: share reveal request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return shares.Result{}, fmt.Errorf("operator client: share service is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return shares.Result{}, errorFromResponse(resp, "operator client: share reveal failed")
	}
	var result shares.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return shares.Result{}, fmt.Errorf("operator client: share reveal response could not be decoded")
	}
	return result, nil
}

func (c *Client) IssueShare(ctx context.Context, route string, expiry sharestate.Expiry) (shares.Result, error) {
	body, _ := json.Marshal(map[string]any{"route": route, "expires": expiry})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/_swobu/shares", bytes.NewReader(body))
	if err != nil {
		return shares.Result{}, fmt.Errorf("operator client: share request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return shares.Result{}, fmt.Errorf("operator client: share service is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return shares.Result{}, errorFromResponse(resp, "operator client: share failed")
	}
	var result shares.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return shares.Result{}, fmt.Errorf("operator client: share response could not be decoded")
	}
	return result, nil
}

func (c *Client) RevokeShare(ctx context.Context, route string) error {
	body, _ := json.Marshal(map[string]string{"route": route})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/_swobu/shares", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("operator client: revoke request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("operator client: share service is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return errorFromResponse(resp, "operator client: revoke failed")
	}
	return nil
}
