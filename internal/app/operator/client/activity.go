package operatorclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/routing"
)

type ActivityProjection struct {
	Scope         ActivityScope          `json:"scope"`
	Exchanges     []ActivityExchange     `json:"exchanges"`
	MCPOperations []ActivityMCPOperation `json:"mcp_operations,omitempty"`
	History       *ledger.Health         `json:"history,omitempty"`
	Page          *ledger.ActivityPage   `json:"page,omitempty"`
}

// ActivityMCPOperation projects a standalone MCP effect without model traffic.
// Transport uncertainty and source identity are independent of client delivery.
type ActivityMCPOperation struct {
	OperationID string               `json:"operation_id"`
	AccessID    string               `json:"access_id"`
	Workspace   string               `json:"workspace"`
	Source      string               `json:"source"`
	Tool        string               `json:"tool"`
	StartedAt   string               `json:"started_at"`
	SettledAt   string               `json:"settled_at,omitempty"`
	Outcome     execution.Outcome    `json:"outcome,omitempty"`
	Occurrence  execution.Occurrence `json:"execution_possibility,omitempty"`
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

// Activity reads one canonical content-free projection with exact scope binding.
// Scoped history cannot claim Node-wide capture health; no retry or fallback
// follows a malformed acknowledgement. Empty workspace explicitly selects Node.
func (c *Client) Activity(ctx context.Context, workspace string) (ActivityProjection, error) {
	return c.activity(ctx, workspace, "", nil)
}

// ActivityByKind reads only the selected canonical evidence class, preserving
// scope and capture-health qualifiers. Unsupported kinds reject before contact.
func (c *Client) ActivityByKind(ctx context.Context, workspace, kind string) (ActivityProjection, error) {
	if kind != "exchange" && kind != "mcp_operation" {
		return ActivityProjection{}, fmt.Errorf("operator client: supported Activity kind required")
	}
	return c.activity(ctx, workspace, kind, nil)
}

// ActivityPageRequest selects retained history, excluding live snapshots.
// Limit bounds each included kind independently; cursors bind kind and scope.
type ActivityPageRequest struct {
	Limit          int
	Kind           string
	ExchangeCursor string
	MCPCursor      string
}

func (c *Client) ActivityPage(ctx context.Context, workspace string, page ActivityPageRequest) (ActivityProjection, error) {
	if page.Limit < 1 || page.Limit > ledger.MaximumPageSize || (page.Kind != "" && page.Kind != "exchange" && page.Kind != "mcp_operation") {
		return ActivityProjection{}, fmt.Errorf("operator client: invalid Activity page")
	}
	for _, boundary := range []struct{ raw, kind string }{{page.ExchangeCursor, "exchange"}, {page.MCPCursor, "mcp_operation"}} {
		if boundary.raw != "" {
			if page.Kind != "" && page.Kind != boundary.kind {
				return ActivityProjection{}, fmt.Errorf("operator client: excluded Activity cursor")
			}
			if _, _, err := ledger.DecodeActivityCursor(boundary.raw, workspace, boundary.kind); err != nil {
				return ActivityProjection{}, fmt.Errorf("operator client: invalid Activity cursor")
			}
		}
	}
	return c.activity(ctx, workspace, page.Kind, &page)
}

func (c *Client) activity(ctx context.Context, workspace, kind string, page *ActivityPageRequest) (ActivityProjection, error) {
	if workspace != "" {
		slug, err := routing.ParseWorkspaceSlug(workspace)
		if err != nil || slug.String() != workspace {
			return ActivityProjection{}, fmt.Errorf("operator client: exact Activity Workspace required")
		}
	}
	path := c.baseURL + "/api/v1/activity"
	if workspace != "" {
		path += "?workspace=" + url.QueryEscape(workspace)
	}
	if kind != "" {
		separator := "?"
		if workspace != "" {
			separator = "&"
		}
		path += separator + "kind=" + kind
	}
	if page != nil {
		separator := "?"
		if workspace != "" || kind != "" {
			separator = "&"
		}
		path += separator + "limit=" + strconv.Itoa(page.Limit)
		if page.ExchangeCursor != "" {
			path += "&exchange_cursor=" + url.QueryEscape(page.ExchangeCursor)
		}
		if page.MCPCursor != "" {
			path += "&mcp_cursor=" + url.QueryEscape(page.MCPCursor)
		}
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
	decoder := json.NewDecoder(resp.Body)
	if decoder.Decode(&projection) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ActivityProjection{}, fmt.Errorf("operator client: activity could not be decoded")
	}
	if err := validateActivityScope(projection, workspace, kind); err != nil {
		return ActivityProjection{}, err
	}
	if page != nil {
		if projection.Page == nil || projection.Page.Mode != "retained" || projection.Page.Limit != page.Limit || len(projection.Exchanges) > page.Limit || len(projection.MCPOperations) > page.Limit {
			return ActivityProjection{}, fmt.Errorf("operator client: invalid Activity page acknowledgement")
		}
		for _, exchange := range projection.Exchanges {
			if exchange.State == "in_progress" {
				return ActivityProjection{}, fmt.Errorf("operator client: live Exchange in retained Activity page")
			}
		}
		for _, boundary := range []struct{ raw, kind string }{{projection.Page.ExchangeNext, "exchange"}, {projection.Page.MCPNext, "mcp_operation"}} {
			if boundary.raw != "" {
				if kind != "" && kind != boundary.kind {
					return ActivityProjection{}, fmt.Errorf("operator client: excluded Activity page cursor")
				}
				_, identity, err := ledger.DecodeActivityCursor(boundary.raw, workspace, boundary.kind)
				if err != nil || (boundary.kind == "exchange" && (len(projection.Exchanges) == 0 || projection.Exchanges[len(projection.Exchanges)-1].ExchangeID != identity)) || (boundary.kind == "mcp_operation" && (len(projection.MCPOperations) == 0 || projection.MCPOperations[len(projection.MCPOperations)-1].OperationID != identity)) {
					return ActivityProjection{}, fmt.Errorf("operator client: invalid Activity page cursor acknowledgement")
				}
			}
		}
	} else if projection.Page != nil {
		return ActivityProjection{}, fmt.Errorf("operator client: unexpected retained Activity acknowledgement")
	}
	return projection, nil
}

func validateActivityScope(projection ActivityProjection, workspace, kind string) error {
	expectedScope := ActivityScope{Kind: "all"}
	if workspace != "" {
		expectedScope = ActivityScope{Kind: "workspace", Workspace: workspace}
	}
	if projection.Scope != expectedScope || projection.Exchanges == nil || (workspace != "" && projection.History != nil) {
		return fmt.Errorf("operator client: invalid Activity scope acknowledgement")
	}
	for _, exchange := range projection.Exchanges {
		if workspace != "" && exchange.Workspace != workspace {
			return fmt.Errorf("operator client: invalid Activity Exchange scope")
		}
	}
	for _, operation := range projection.MCPOperations {
		if workspace != "" && operation.Workspace != workspace {
			return fmt.Errorf("operator client: invalid Activity MCP scope")
		}
	}
	if (kind == "exchange" && len(projection.MCPOperations) != 0) || (kind == "mcp_operation" && len(projection.Exchanges) != 0) {
		return fmt.Errorf("operator client: invalid Activity kind acknowledgement")
	}
	return nil
}
