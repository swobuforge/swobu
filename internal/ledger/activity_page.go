package ledger

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/execution"
)

// ActivityPageQuery selects retained pages independently of live snapshots.
// Each included kind has its own exclusive settlement/identity boundary.
type ActivityPageQuery struct {
	Limit          int
	Kind           string
	Exchange       Cursor
	MCP            MCPCursor
	ExchangeFilter ExchangeFilter
}

// NewActivityPageQuery validates the transport-independent retained-page intent.
// Limit zero selects the current snapshot and cannot carry retained cursors.
// Both Control and Admin MCP bind opaque cursors to the same Workspace/kind.
func NewActivityPageQuery(workspace, kind string, limit int, exchangeCursor, mcpCursor string) (ActivityPageQuery, error) {
	page := ActivityPageQuery{Kind: kind, Limit: limit}
	invalid := errors.New("invalid Activity page")
	if kind != "" && kind != "exchange" && kind != "mcp_operation" {
		return page, invalid
	}
	if limit < 0 || limit > MaximumPageSize || (limit == 0 && (exchangeCursor != "" || mcpCursor != "")) {
		return page, invalid
	}
	if exchangeCursor != "" {
		if kind == "mcp_operation" {
			return page, invalid
		}
		settled, identity, err := DecodeActivityCursor(exchangeCursor, workspace, "exchange")
		if err != nil {
			return page, err
		}
		exchange, err := execution.ParseExchangeID(identity)
		if err != nil {
			return page, err
		}
		page.Exchange, err = NewCursor(settled, exchange)
		if err != nil {
			return page, err
		}
	}
	if mcpCursor != "" {
		if kind == "exchange" {
			return page, invalid
		}
		settled, identity, err := DecodeActivityCursor(mcpCursor, workspace, "mcp_operation")
		if err != nil {
			return page, err
		}
		page.MCP, err = NewMCPCursor(settled, identity)
		if err != nil {
			return page, err
		}
	}
	return page, nil
}

type activityCursorRecord struct {
	Workspace string `json:"workspace"`
	Kind      string `json:"kind"`
	Settled   string `json:"settled"`
	Identity  string `json:"identity"`
}

// EncodeActivityCursor binds an opaque boundary to its Workspace and kind.
// It grants no authority and does not promise retained or complete history.
func EncodeActivityCursor(workspace, kind string, settled time.Time, identity string) string {
	body, _ := json.Marshal(activityCursorRecord{workspace, kind, settled.UTC().Format(time.RFC3339Nano), identity})
	return base64.RawURLEncoding.EncodeToString(body)
}

func DecodeActivityCursor(raw, workspace, kind string) (time.Time, string, error) {
	invalid := errors.New("invalid Activity cursor binding")
	if len(raw) == 0 || len(raw) > 2048 {
		return time.Time{}, "", invalid
	}
	body, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", invalid
	}
	var record activityCursorRecord
	if json.Unmarshal(body, &record) != nil || record.Workspace != workspace || record.Kind != kind || record.Identity == "" {
		return time.Time{}, "", invalid
	}
	settled, err := time.Parse(time.RFC3339Nano, record.Settled)
	if err != nil || settled.IsZero() || EncodeActivityCursor(workspace, kind, settled, record.Identity) != raw {
		return time.Time{}, "", invalid
	}
	if kind == "exchange" {
		if _, err := execution.ParseExchangeID(record.Identity); err != nil {
			return time.Time{}, "", invalid
		}
	} else if kind != "mcp_operation" {
		return time.Time{}, "", invalid
	}
	return settled, record.Identity, nil
}

// ActivityPage describes only retained rows. Empty Next fields mean observed
// exhaustion for that kind, not absence of live work or capture gaps.
type ActivityPage struct {
	Limit        int    `json:"limit"`
	Mode         string `json:"mode"`
	ExchangeNext string `json:"exchange_next,omitempty"`
	MCPNext      string `json:"mcp_next,omitempty"`
}
