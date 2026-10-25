package executionevidence

import (
	"time"

	"github.com/swobuforge/swobu/internal/domain/execution"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
)

// OperationDetail distinguishes model Exchanges from independent MCP calls.
// It projects lifecycle metadata only, never prompts, arguments or results.
type OperationDetail struct {
	Kind     string          `json:"kind"`
	Evidence string          `json:"evidence"`
	Exchange *ExchangeDetail `json:"exchange,omitempty"`
	MCP      *MCPOperation   `json:"mcp_operation,omitempty"`
}

// UpstreamExecution counts recorded lifecycle facts, not provider invoices,
// externally committed side effects, or proof that unrecorded dispatches did not
// occur. All counts are derived; no new durable evidence is fabricated.
type UpstreamExecution struct {
	Recorded        int `json:"recorded_invocations"`
	Occurred        int `json:"occurred"`
	MayHaveOccurred int `json:"may_have_occurred"`
	DidNotOccur     int `json:"did_not_occur"`
	Unsettled       int `json:"unsettled"`
}

type ExchangeDetail struct {
	Identity    string             `json:"exchange_id"`
	Access      string             `json:"access_id"`
	Workspace   string             `json:"workspace"`
	Started     time.Time          `json:"started_at"`
	Settled     time.Time          `json:"settled_at,omitzero"`
	Result      string             `json:"delivery_result,omitempty"`
	ErrorCode   string             `json:"error_code,omitempty"`
	Upstream    UpstreamExecution  `json:"upstream_execution"`
	Status      int                `json:"http_status,omitempty"`
	Invocations []InvocationDetail `json:"invocations"`
}

type InvocationDetail struct {
	ConnectionID       string `json:"connection_id,omitempty"`
	ConnectionRevision uint64 `json:"connection_revision,omitempty"`

	Identity   string               `json:"invocation_id"`
	Kind       string               `json:"kind"`
	Started    time.Time            `json:"started_at"`
	Settled    time.Time            `json:"settled_at,omitzero"`
	Source     string               `json:"source,omitempty"`
	Tool       string               `json:"tool,omitempty"`
	Provider   string               `json:"provider,omitempty"`
	Route      string               `json:"route,omitempty"`
	Model      string               `json:"model,omitempty"`
	Target     string               `json:"target,omitempty"`
	Version    uint64               `json:"target_version,omitempty"`
	Purpose    string               `json:"purpose,omitempty"`
	Outcome    execution.Outcome    `json:"outcome,omitempty"`
	Occurrence execution.Occurrence `json:"execution_possibility,omitempty"`
	Failure    string               `json:"failure,omitempty"`
}

func ProjectExchangeDetail(record ExchangeLifecycle, evidence string) OperationDetail {
	row := &ExchangeDetail{Identity: record.Started.ID().String(), Access: record.Started.AccessID().String(), Workspace: record.Started.Workspace().String(), Started: record.Started.StartedAt(), Invocations: make([]InvocationDetail, 0, len(record.Invocations))}
	if record.Settled != nil {
		row.Settled, row.Result, row.Status, row.ErrorCode = record.Settled.SettledAt(), string(record.Settled.Result()), record.Settled.Status(), string(record.Settled.ErrorCode())
	}
	for _, invocation := range record.Invocations {
		detail := InvocationDetail{Identity: invocation.Started.ID().String(), Started: invocation.Started.StartedAt()}
		row.Upstream.Recorded++
		switch started := invocation.Started.(type) {
		case domain.ModelInvocationStarted:
			model := started.Model()
			id, revision, _ := model.ConnectionIdentity()
			detail.ConnectionID, detail.ConnectionRevision = id.String(), revision
			detail.Kind, detail.Route, detail.Provider, detail.Model, detail.Target, detail.Version, detail.Purpose = "model", model.Route().String(), string(model.Provider()), model.Model().String(), model.Target().String(), uint64(model.Version()), string(model.Purpose())
		case domain.MCPInvocationStarted:
			detail.Kind, detail.Source, detail.Tool = "mcp_tool", started.MCP().Source().String(), started.MCP().Tool().String()
			id, revision, _ := started.MCP().ConnectionIdentity()
			detail.ConnectionID, detail.ConnectionRevision = id.String(), revision
		}
		if invocation.Settled == nil {
			row.Upstream.Unsettled++
		} else {
			switch invocation.Settled.Occurrence() {
			case execution.Occurred:
				row.Upstream.Occurred++
			case execution.MayHaveOccurred:
				row.Upstream.MayHaveOccurred++
			case execution.DidNotOccur:
				row.Upstream.DidNotOccur++
			}
			detail.Settled, detail.Outcome, detail.Occurrence = invocation.Settled.SettledAt(), invocation.Settled.Outcome(), invocation.Settled.Occurrence()
			switch settled := invocation.Settled.(type) {
			case domain.ModelInvocationSettled:
				detail.Failure = string(settled.Failure())
			case domain.MCPInvocationSettled:
				detail.Failure = string(settled.Failure())
			}
		}
		row.Invocations = append(row.Invocations, detail)
	}
	return OperationDetail{Kind: "exchange", Evidence: evidence, Exchange: row}
}
