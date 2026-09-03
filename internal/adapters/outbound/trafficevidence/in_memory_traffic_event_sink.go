package trafficevidence

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/domain/trafficevidence"
)

type StoreConfig struct {
	MaxEvents   int
	RecentLimit int
}

// StatusProjection is the minimal read model derived from immutable traffic
// events plus runtime health truth.
type StatusProjection struct {
	State          string             `json:"state"`
	WorkspaceCount int                `json:"workspace_count"`
	Scope          ProjectionScope    `json:"scope"`
	Counters       StatusCounters     `json:"counters"`
	RecentTraffic  []RecentTrafficRow `json:"recent_traffic"`
}

type ProjectionScopeKind string

const (
	ProjectionScopeAll       ProjectionScopeKind = "all"
	ProjectionScopeWorkspace ProjectionScopeKind = "workspace"
)

type ProjectionScope struct {
	Kind      ProjectionScopeKind `json:"kind"`
	Workspace string              `json:"workspace,omitempty"`
}

// StatusCounters are summaries only; they must stay derivable from traffic
// events rather than becoming an independent truth source.
type StatusCounters struct {
	Count2xx int            `json:"count_2xx"`
	Count429 int            `json:"count_429"`
	Count4xx int            `json:"count_4xx"`
	Count5xx int            `json:"count_5xx"`
	PerModel map[string]int `json:"per_model"`
}

type RecentTrafficRow struct {
	RequestID         string                              `json:"request_id"`
	Workspace         string                              `json:"workspace"`
	ClientHandler     string                              `json:"client_handler,omitempty"`
	ClientProtocol    string                              `json:"client_protocol,omitempty"`
	ClientFamily      string                              `json:"client_family,omitempty"`
	NormalizedOp      string                              `json:"normalized_op,omitempty"`
	Route             string                              `json:"route"`
	Result            string                              `json:"result"`
	StatusCode        int                                 `json:"status_code"`
	AttemptCount      int                                 `json:"attempt_count"`
	FallbackRecovered bool                                `json:"fallback_recovered"`
	ObservedAt        string                              `json:"observed_at,omitempty"`
	Timing            *RecentTrafficTimingSnapshot        `json:"timing,omitempty"`
	TokenUsage        *RecentTrafficTokenUsageSnapshot    `json:"token_usage,omitempty"`
	ReusablePrefix    RecentTrafficReusablePrefixSnapshot `json:"reusable_prefix"`
	// TODO(execution-system): Flattened token fields are preserved for continuity with existing
	// trafficevidence integration tests and older readers.
	InputTokens           *int                          `json:"-"`
	OutputTokens          *int                          `json:"-"`
	CacheReadTokens       *int                          `json:"-"`
	CacheWriteTokens      *int                          `json:"-"`
	ModelRequested        string                        `json:"model_requested,omitempty"`
	ModelResolved         string                        `json:"model_resolved,omitempty"`
	ModelResolutionMode   string                        `json:"model_resolution_mode,omitempty"`
	WorkspaceRouteModelID string                        `json:"workspace_route_model,omitempty"`
	ProviderSpec          string                        `json:"provider_spec,omitempty"`
	ProviderModel         string                        `json:"provider_model,omitempty"`
	TargetProtocol        string                        `json:"target_protocol,omitempty"`
	TargetVersion         uint64                        `json:"target_version,omitempty"`
	Mutations             []trafficevidence.Mutation    `json:"wire_patch_mutations,omitempty"`
	ExchangeDiagnostics   []string                      `json:"exchange_diagnostics,omitempty"`
	StageReports          []trafficevidence.StageReport `json:"exchange_stage_reports,omitempty"`
}

type RecentTrafficTimingSnapshot struct {
	TTFBMillis *int `json:"ttfb_millis,omitempty"`
	DurMillis  *int `json:"dur_millis,omitempty"`
}

func (s RecentTrafficTimingSnapshot) TTFBMillisValue() *int {
	return s.TTFBMillis
}

func (s RecentTrafficTimingSnapshot) DurationMillisValue() *int {
	return s.DurMillis
}

type RecentTrafficTokenUsageSnapshot struct {
	InputTokens      *int `json:"input_tokens,omitempty"`
	OutputTokens     *int `json:"output_tokens,omitempty"`
	ReasoningTokens  *int `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  *int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
}

type RecentTrafficReusablePrefixSnapshot struct {
	State      string `json:"state"`
	ChangeKind string `json:"change_kind,omitempty"`
}

type ProjectionInput struct {
	State          string
	WorkspaceCount int
	RecentLimit    int
	Scope          ProjectionScope
}

type InMemoryTrafficEventSink struct {
	maxEvents   int
	recentLimit int

	mu     sync.RWMutex
	events []stampedTrafficEvent
}

type stampedTrafficEvent struct {
	event      trafficevidence.TrafficEvent
	observedAt time.Time
}

func NewTrafficEventStore(cfg StoreConfig) *InMemoryTrafficEventSink {
	maxEvents := cfg.MaxEvents
	if maxEvents <= 0 {
		maxEvents = 512
	}
	recentLimit := cfg.RecentLimit
	if recentLimit <= 0 {
		recentLimit = 20
	}
	return &InMemoryTrafficEventSink{
		maxEvents:   maxEvents,
		recentLimit: recentLimit,
	}
}

func (s *InMemoryTrafficEventSink) Append(_ context.Context, event trafficevidence.TrafficEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, stampedTrafficEvent{
		event:      event,
		observedAt: time.Now(),
	})
	if len(s.events) > s.maxEvents {
		s.events = slices.Clone(s.events[len(s.events)-s.maxEvents:])
	}
}

func (s *InMemoryTrafficEventSink) Events() []stampedTrafficEvent {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.events)
}

func (s *InMemoryTrafficEventSink) ProjectStatus(input ProjectionInput) StatusProjection {
	scope := normalizeProjectionScope(input.Scope)
	if s == nil {
		return StatusProjection{
			State:          input.State,
			WorkspaceCount: input.WorkspaceCount,
			Scope:          scope,
			Counters: StatusCounters{
				PerModel: map[string]int{},
			},
		}
	}
	latest := reconcileLatestByRequestID(s.Events())
	recentLimit := input.RecentLimit
	if recentLimit <= 0 {
		recentLimit = s.recentLimit
	}

	projection := StatusProjection{
		State:          input.State,
		WorkspaceCount: input.WorkspaceCount,
		Scope:          scope,
		Counters: StatusCounters{
			PerModel: map[string]int{},
		},
		RecentTraffic: make([]RecentTrafficRow, 0, min(recentLimit, len(latest))),
	}
	for _, event := range latest {
		if !scope.includesWorkspace(event.event.Workspace()) {
			continue
		}
		if event.event.Result().IsTerminal() {
			classifyCounters(&projection.Counters, event.event)
		}
		if len(projection.RecentTraffic) < recentLimit {
			projection.RecentTraffic = append(projection.RecentTraffic, recentTrafficRow(event))
		}
	}
	return projection
}

// Reconciliation keeps immutable in-flight facts intact and simply projects the
// latest known event per request ID when operators ask for current status.
func reconcileLatestByRequestID(events []stampedTrafficEvent) []stampedTrafficEvent {
	if len(events) == 0 {
		return nil
	}
	latest := make([]stampedTrafficEvent, 0, len(events))
	seen := make(map[string]struct{}, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		requestID := events[i].event.RequestID().String()
		if _, ok := seen[requestID]; ok {
			continue
		}
		seen[requestID] = struct{}{}
		latest = append(latest, events[i])
	}
	return latest
}

func classifyCounters(counters *StatusCounters, event trafficevidence.TrafficEvent) {
	statusCode := event.StatusCode()
	switch {
	case statusCode >= 200 && statusCode < 300:
		counters.Count2xx++
	case statusCode == 429:
		counters.Count429++
	case statusCode >= 400 && statusCode < 500:
		counters.Count4xx++
	case statusCode >= 500:
		counters.Count5xx++
	}
	if model := event.Route().Model(); model != "" {
		counters.PerModel[model]++
	}
}

func recentTrafficRow(event stampedTrafficEvent) RecentTrafficRow {
	trafficEvent := event.event
	row := RecentTrafficRow{Workspace: trafficEvent.Workspace(),
		ClientHandler:         string(trafficEvent.ClientHandler()),
		ClientProtocol:        string(trafficEvent.ClientProtocol()),
		ClientFamily:          string(trafficEvent.ClientFamily()),
		NormalizedOp:          string(trafficEvent.NormalizedOp()),
		Route:                 trafficEvent.Route().String(),
		Result:                trafficEvent.Result().String(),
		StatusCode:            trafficEvent.StatusCode(),
		AttemptCount:          trafficEvent.AttemptCount(),
		FallbackRecovered:     trafficEvent.FallbackRecovered(),
		ModelRequested:        trafficEvent.ModelRequested(),
		ModelResolved:         trafficEvent.ModelResolved(),
		ModelResolutionMode:   trafficEvent.ModelResolutionMode(),
		WorkspaceRouteModelID: trafficEvent.WorkspaceRouteModelID(),
		ProviderSpec:          string(trafficEvent.ProviderSpec()),
		ProviderModel:         trafficEvent.ProviderModel(),
		TargetProtocol:        trafficEvent.TargetProtocol().String(),
		TargetVersion:         uint64(trafficEvent.TargetVersion()),
		Mutations:             trafficEvent.Mutations(),
		ExchangeDiagnostics:   trafficEvent.ExchangeDiagnostics(),
		StageReports:          trafficEvent.StageReports(),
		ReusablePrefix:        recentTrafficPrefix(trafficEvent.ReusablePrefix()),
	}
	if !event.observedAt.IsZero() {
		row.ObservedAt = event.observedAt.Format("15:04:05")
	}
	timing := RecentTrafficTimingSnapshot{}
	if ttfbMS, ok := trafficEvent.Timing().TTFBMillis(); ok {
		timing.TTFBMillis = &ttfbMS
	}
	if durMS, ok := trafficEvent.Timing().DurationMillis(); ok {
		timing.DurMillis = &durMS
	}
	if timing.TTFBMillis != nil || timing.DurMillis != nil {
		row.Timing = &timing
	}
	usage := RecentTrafficTokenUsageSnapshot{}
	if inputTokens, ok := trafficEvent.TokenUsage().InputTokens(); ok {
		usage.InputTokens = &inputTokens
	}
	if outputTokens, ok := trafficEvent.TokenUsage().OutputTokens(); ok {
		usage.OutputTokens = &outputTokens
	}
	if reasoningTokens, ok := trafficEvent.TokenUsage().ReasoningTokens(); ok {
		usage.ReasoningTokens = &reasoningTokens
	}
	if cacheReadTokens, ok := trafficEvent.TokenUsage().CacheReadTokens(); ok {
		usage.CacheReadTokens = &cacheReadTokens
	}
	if cacheWriteTokens, ok := trafficEvent.TokenUsage().CacheWriteTokens(); ok {
		usage.CacheWriteTokens = &cacheWriteTokens
	}
	if usage.InputTokens != nil || usage.OutputTokens != nil || usage.ReasoningTokens != nil || usage.CacheReadTokens != nil || usage.CacheWriteTokens != nil {
		row.TokenUsage = &usage
		row.InputTokens = usage.InputTokens
		row.OutputTokens = usage.OutputTokens
		row.CacheReadTokens = usage.CacheReadTokens
		row.CacheWriteTokens = usage.CacheWriteTokens
	}
	return row
}

func recentTrafficPrefix(evidence trafficevidence.ReusablePrefixEvidence) RecentTrafficReusablePrefixSnapshot {
	snapshot := RecentTrafficReusablePrefixSnapshot{State: string(evidence.State())}
	if kind, ok := evidence.ChangeKind(); ok {
		snapshot.ChangeKind = string(kind)
	}
	return snapshot
}

func min(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func normalizeProjectionScope(scope ProjectionScope) ProjectionScope {
	switch scope.Kind {
	case ProjectionScopeWorkspace:
		if scope.Workspace != "" {
			return scope
		}
		return ProjectionScope{Kind: ProjectionScopeAll}
	case ProjectionScopeAll:
		return ProjectionScope{Kind: ProjectionScopeAll}
	default:
		return ProjectionScope{Kind: ProjectionScopeAll}
	}
}

func (s ProjectionScope) includesWorkspace(workspace string) bool {
	if s.Kind == ProjectionScopeWorkspace {
		return workspace == s.Workspace
	}
	return true
}
