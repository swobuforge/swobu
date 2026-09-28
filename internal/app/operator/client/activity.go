package operatorclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type ActivityProjection struct {
	Scope     ActivityScope      `json:"scope"`
	Exchanges []ActivityExchange `json:"exchanges"`
}
type ActivityScope struct {
	Kind      string `json:"kind"`
	Workspace string `json:"workspace,omitempty"`
}
type ActivityExchange struct {
	ExchangeID     string              `json:"exchange_id"`
	AccessID       string              `json:"access_id"`
	Workspace      string              `json:"workspace"`
	ClientFamily   string              `json:"client_family,omitempty"`
	ClientProtocol string              `json:"client_protocol,omitempty"`
	Operation      string              `json:"operation,omitempty"`
	Route          string              `json:"route,omitempty"`
	State          string              `json:"state"`
	HTTPStatus     int                 `json:"http_status,omitempty"`
	AttemptCount   int                 `json:"attempt_count"`
	Provider       string              `json:"provider,omitempty"`
	Model          string              `json:"model,omitempty"`
	Protocol       string              `json:"protocol,omitempty"`
	TargetVersion  uint64              `json:"target_version,omitempty"`
	StartedAt      string              `json:"started_at"`
	DurationMS     *int                `json:"duration_ms,omitempty"`
	TokenUsage     *ActivityTokenUsage `json:"token_usage,omitempty"`
}

type ActivityTokenUsage struct {
	InputTokens      *int `json:"input_tokens,omitempty"`
	OutputTokens     *int `json:"output_tokens,omitempty"`
	ReasoningTokens  *int `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  *int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
}

func (c *Client) Activity(ctx context.Context, workspace string) (ActivityProjection, error) {
	path := c.baseURL + "/_swobu/activity"
	if workspace != "" {
		path += "?workspace=" + url.QueryEscape(workspace)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return ActivityProjection{}, fmt.Errorf("operator client: activity request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ActivityProjection{}, fmt.Errorf("operator client: activity unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ActivityProjection{}, errorFromResponse(resp, "operator client: activity failed")
	}
	var projection ActivityProjection
	if err := json.NewDecoder(resp.Body).Decode(&projection); err != nil {
		return ActivityProjection{}, fmt.Errorf("operator client: activity could not be decoded")
	}
	return projection, nil
}
