package executionevidence

import (
	"sort"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
)

type ProjectionScopeKind string

const (
	ProjectionScopeAll       ProjectionScopeKind = "all"
	ProjectionScopeWorkspace ProjectionScopeKind = "workspace"
)

type ProjectionScope struct {
	Kind      ProjectionScopeKind `json:"kind"`
	Workspace string              `json:"workspace,omitempty"`
}
type ActivityTokenUsage struct {
	InputTokens      *int `json:"input_tokens,omitempty"`
	OutputTokens     *int `json:"output_tokens,omitempty"`
	ReasoningTokens  *int `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  *int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
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
	StartedAt      time.Time           `json:"started_at"`
	DurationMS     *int                `json:"duration_ms,omitempty"`
	TokenUsage     *ActivityTokenUsage `json:"token_usage,omitempty"`
}
type ActivityProjection struct {
	Scope     ProjectionScope    `json:"scope"`
	Exchanges []ActivityExchange `json:"exchanges"`
}

type ProjectionInput struct {
	Scope ProjectionScope
}

func (s *Store) ProjectActivity(input ProjectionInput) ActivityProjection {
	result := ActivityProjection{Scope: input.Scope, Exchanges: make([]ActivityExchange, 0)}
	if s == nil {
		return result
	}
	all := append(s.Active(), s.Settled()...)
	sort.Slice(all, func(i, j int) bool { return all[i].Started.StartedAt().After(all[j].Started.StartedAt()) })
	for _, record := range all {
		if input.Scope.Kind == ProjectionScopeWorkspace && record.Started.Workspace().String() != input.Scope.Workspace {
			continue
		}
		row := ActivityExchange{ExchangeID: record.Started.ID().String(), AccessID: record.Started.AccessID().String(), Workspace: record.Started.Workspace().String(), ClientFamily: string(record.Started.ClientFamily()), ClientProtocol: record.Started.ClientProtocol().String(), Operation: string(record.Started.Operation()), State: "in_progress"}
		var usages []canonical.TokenUsage
		for _, invocation := range record.Invocations {
			if started, ok := invocation.Started.(domain.ModelInvocationStarted); ok && started.Model().Purpose() == domain.ExchangeAttempt {
				m := started.Model()
				row.AttemptCount++
				row.Route = m.Route().String()
				row.Provider = string(m.Provider())
				row.Model = m.Model().String()
				row.Protocol = m.Protocol().String()
				row.TargetVersion = uint64(m.Version())
				if settled, ok := invocation.Settled.(domain.ModelInvocationSettled); ok {
					usages = append(usages, settled.Usage())
				}
			}
		}
		if len(usages) > 0 {
			row.TokenUsage = activityTokenUsage(canonical.SumTokenUsage(usages...))
		}
		if record.Settled != nil {
			row.State = deliveryResultClass(record.Settled.Result())
			row.HTTPStatus = record.Settled.Status()
			elapsed := int(record.Settled.SettledAt().Sub(record.Started.StartedAt()).Milliseconds())
			if elapsed < 0 {
				elapsed = 0
			}
			row.DurationMS = &elapsed
		}
		result.Exchanges = append(result.Exchanges, row)
	}
	return result
}

// Activity token use includes only route-attempt model invocations;
// characterization and MCP operations remain separate causal evidence.
func activityTokenUsage(usage canonical.TokenUsage) *ActivityTokenUsage {
	toPtr := func(v int, ok bool) *int {
		if !ok {
			return nil
		}
		return &v
	}
	in, iok := usage.InputTokens()
	out, ook := usage.OutputTokens()
	reason, rok := usage.ReasoningTokens()
	read, rdok := usage.CacheReadTokens()
	write, wok := usage.CacheWriteTokens()
	if !rdok && wok {
		read, rdok = 0, true
	}
	return &ActivityTokenUsage{InputTokens: toPtr(in, iok), OutputTokens: toPtr(out, ook), ReasoningTokens: toPtr(reason, rok), CacheReadTokens: toPtr(read, rdok), CacheWriteTokens: toPtr(write, wok)}
}

func deliveryResultClass(result delivery.ResultKind) string {
	switch result {
	case delivery.Succeeded:
		return "succeeded"
	case delivery.ClientCancelled:
		return "cancelled"
	default:
		return "failed"
	}
}
