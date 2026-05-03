package evidence

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
)

type StoreConfig struct {
	MaxEvents   int
	RecentLimit int
}

// StatusProjection is the minimal read model derived from immutable traffic
// events plus runtime health truth.
type StatusProjection struct {
	State         string             `json:"state"`
	EndpointCount int                `json:"endpoint_count"`
	Scope         ProjectionScope    `json:"scope"`
	Counters      StatusCounters     `json:"counters"`
	RecentTraffic []RecentTrafficRow `json:"recent_traffic"`
}

type ProjectionScopeKind string

const (
	ProjectionScopeAll      ProjectionScopeKind = "all"
	ProjectionScopeEndpoint ProjectionScopeKind = "endpoint"
)

type ProjectionScope struct {
	Kind     ProjectionScopeKind `json:"kind"`
	Endpoint string              `json:"endpoint,omitempty"`
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
	RequestID           string `json:"request_id"`
	Endpoint            string `json:"endpoint"`
	ClientHandler       string `json:"client_handler,omitempty"`
	ClientProtocol      string `json:"client_protocol,omitempty"`
	IngressFamily       string `json:"ingress_family,omitempty"`
	NormalizedOp        string `json:"normalized_op,omitempty"`
	Route               string `json:"route"`
	Result              string `json:"result"`
	StatusCode          int    `json:"status_code"`
	ObservedAt          string `json:"observed_at,omitempty"`
	TTFBMillis          *int   `json:"ttfb_millis,omitempty"`
	DurMillis           *int   `json:"dur_millis,omitempty"`
	InputTokens         *int   `json:"input_tokens,omitempty"`
	OutputTokens        *int   `json:"output_tokens,omitempty"`
	CacheReadTokens     *int   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens    *int   `json:"cache_write_tokens,omitempty"`
	ModelRequested      string `json:"model_requested,omitempty"`
	ModelResolved       string `json:"model_resolved,omitempty"`
	ModelResolutionMode string `json:"model_resolution_mode,omitempty"`
}

type ProjectionInput struct {
	State         string
	EndpointCount int
	RecentLimit   int
	Scope         ProjectionScope
}

type RequestEvidenceSinkStore struct {
	maxEvents   int
	recentLimit int

	mu     sync.RWMutex
	events []stampedTrafficEvent
}

type stampedTrafficEvent struct {
	event      runtimeevidence.TrafficEvent
	observedAt time.Time
}

func NewStore(cfg StoreConfig) *RequestEvidenceSinkStore {
	maxEvents := cfg.MaxEvents
	if maxEvents <= 0 {
		maxEvents = 512
	}
	recentLimit := cfg.RecentLimit
	if recentLimit <= 0 {
		recentLimit = 20
	}
	return &RequestEvidenceSinkStore{
		maxEvents:   maxEvents,
		recentLimit: recentLimit,
	}
}

func (s *RequestEvidenceSinkStore) Append(_ context.Context, event runtimeevidence.TrafficEvent) {
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

func (s *RequestEvidenceSinkStore) Events() []stampedTrafficEvent {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.events)
}

func (s *RequestEvidenceSinkStore) ProjectStatus(input ProjectionInput) StatusProjection {
	scope := normalizeProjectionScope(input.Scope)
	if s == nil {
		return StatusProjection{
			State:         input.State,
			EndpointCount: input.EndpointCount,
			Scope:         scope,
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
		State:         input.State,
		EndpointCount: input.EndpointCount,
		Scope:         scope,
		Counters: StatusCounters{
			PerModel: map[string]int{},
		},
		RecentTraffic: make([]RecentTrafficRow, 0, min(recentLimit, len(latest))),
	}
	for _, event := range latest {
		if !scope.includesEndpoint(event.event.Endpoint()) {
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

func classifyCounters(counters *StatusCounters, event runtimeevidence.TrafficEvent) {
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
	evidence := event.event
	row := RecentTrafficRow{
		RequestID:           evidence.RequestID().String(),
		Endpoint:            evidence.Endpoint(),
		ClientHandler:       string(evidence.ClientHandler()),
		ClientProtocol:      string(evidence.ClientProtocol()),
		IngressFamily:       string(evidence.IngressFamily()),
		NormalizedOp:        string(evidence.NormalizedOp()),
		Route:               evidence.Route().String(),
		Result:              evidence.Result().String(),
		StatusCode:          evidence.StatusCode(),
		ModelRequested:      evidence.ModelRequested(),
		ModelResolved:       evidence.ModelResolved(),
		ModelResolutionMode: evidence.ModelResolutionMode(),
	}
	if !event.observedAt.IsZero() {
		row.ObservedAt = event.observedAt.Format("15:04:05")
	}
	if ttfbMS, ok := evidence.Timing().TTFBMillis(); ok {
		row.TTFBMillis = &ttfbMS
	}
	if durMS, ok := evidence.Timing().DurationMillis(); ok {
		row.DurMillis = &durMS
	}
	if inputTokens, ok := evidence.TokenUsage().InputTokens(); ok {
		row.InputTokens = &inputTokens
	}
	if outputTokens, ok := evidence.TokenUsage().OutputTokens(); ok {
		row.OutputTokens = &outputTokens
	}
	if cacheReadTokens, ok := evidence.TokenUsage().CacheReadTokens(); ok {
		row.CacheReadTokens = &cacheReadTokens
	}
	if cacheWriteTokens, ok := evidence.TokenUsage().CacheWriteTokens(); ok {
		row.CacheWriteTokens = &cacheWriteTokens
	}
	return row
}

func min(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func normalizeProjectionScope(scope ProjectionScope) ProjectionScope {
	switch scope.Kind {
	case ProjectionScopeEndpoint:
		if scope.Endpoint != "" {
			return scope
		}
		return ProjectionScope{Kind: ProjectionScopeAll}
	case ProjectionScopeAll:
		return ProjectionScope{Kind: ProjectionScopeAll}
	default:
		return ProjectionScope{Kind: ProjectionScopeAll}
	}
}

func (s ProjectionScope) includesEndpoint(endpoint string) bool {
	if s.Kind == ProjectionScopeEndpoint {
		return endpoint == s.Endpoint
	}
	return true
}
