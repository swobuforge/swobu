package runtimeevidence_test

import (
	"context"
	"testing"

	evidencestore "github.com/metrofun/swobu/internal/adapters/outbound/evidence"
	"github.com/metrofun/swobu/internal/domain/runtimeevidence"
)

func TestStore_ProjectStatus_ReconcilesByRequestIDAndKeepsRecentNewestFirst(t *testing.T) {
	store := evidencestore.NewStore(evidencestore.StoreConfig{RecentLimit: 2})

	req1 := mustRequestID(t, "req-1")
	req2 := mustRequestID(t, "req-2")
	req3 := mustRequestID(t, "req-3")

	store.Append(context.Background(), mustInflightEvent(t, req1, "alpha", "backend-a", "gpt-4.1"))
	store.Append(context.Background(), mustTerminalEvent(t, req1, "alpha", "backend-a", "gpt-4.1", runtimeevidence.ResultClassSuccess, 200))
	store.Append(context.Background(), mustTerminalEvent(t, req2, "alpha", "backend-b", "", runtimeevidence.ResultClassBackendError, 429))
	store.Append(context.Background(), mustInflightEvent(t, req3, "alpha", "backend-c", ""))

	projection := store.ProjectStatus(evidencestore.ProjectionInput{
		State:         "healthy",
		EndpointCount: 1,
		Scope:         evidencestore.ProjectionScope{Kind: evidencestore.ProjectionScopeAll},
	})

	if projection.Counters.Count2xx != 1 {
		t.Fatalf("2xx count = %d, want 1", projection.Counters.Count2xx)
	}
	if projection.Counters.Count429 != 1 {
		t.Fatalf("429 count = %d, want 1", projection.Counters.Count429)
	}
	if got := projection.Counters.PerModel["gpt-4.1"]; got != 1 {
		t.Fatalf("per-model count = %d, want 1", got)
	}
	if len(projection.RecentTraffic) != 2 {
		t.Fatalf("recent traffic len = %d, want 2", len(projection.RecentTraffic))
	}
	if got := projection.RecentTraffic[0].RequestID; got != "req-3" {
		t.Fatalf("recent[0].request_id = %q, want %q", got, "req-3")
	}
	if got := projection.RecentTraffic[0].Result; got != runtimeevidence.ResultClassInProgress.String() {
		t.Fatalf("recent[0].result = %q, want %q", got, runtimeevidence.ResultClassInProgress)
	}
	if got := projection.RecentTraffic[1].RequestID; got != "req-2" {
		t.Fatalf("recent[1].request_id = %q, want %q", got, "req-2")
	}
}

func TestStore_ProjectStatus_EndpointScopeFiltersRowsAndCounters(t *testing.T) {
	store := evidencestore.NewStore(evidencestore.StoreConfig{RecentLimit: 10})

	req1 := mustRequestID(t, "req-a")
	req2 := mustRequestID(t, "req-b")

	store.Append(context.Background(), mustTerminalEvent(t, req1, "alpha", "backend-a", "gpt-4.1", runtimeevidence.ResultClassSuccess, 200))
	store.Append(context.Background(), mustTerminalEvent(t, req2, "beta", "backend-b", "", runtimeevidence.ResultClassBackendError, 500))

	projection := store.ProjectStatus(evidencestore.ProjectionInput{
		State:         "healthy",
		EndpointCount: 2,
		Scope: evidencestore.ProjectionScope{
			Kind:     evidencestore.ProjectionScopeEndpoint,
			Endpoint: "alpha",
		},
	})

	if got := projection.Scope.Kind; got != evidencestore.ProjectionScopeEndpoint {
		t.Fatalf("scope kind = %q, want %q", got, evidencestore.ProjectionScopeEndpoint)
	}
	if got := projection.Scope.Endpoint; got != "alpha" {
		t.Fatalf("scope endpoint = %q, want %q", got, "alpha")
	}
	if projection.Counters.Count2xx != 1 {
		t.Fatalf("2xx count = %d, want 1", projection.Counters.Count2xx)
	}
	if projection.Counters.Count5xx != 0 {
		t.Fatalf("5xx count = %d, want 0", projection.Counters.Count5xx)
	}
	if len(projection.RecentTraffic) != 1 {
		t.Fatalf("recent traffic len = %d, want 1", len(projection.RecentTraffic))
	}
	if got := projection.RecentTraffic[0].Endpoint; got != "alpha" {
		t.Fatalf("recent[0].endpoint = %q, want %q", got, "alpha")
	}
}

func TestStore_ProjectStatus_RecentTrafficIncludesTokenUsageWhenPresent(t *testing.T) {
	store := evidencestore.NewStore(evidencestore.StoreConfig{RecentLimit: 10})
	req := mustRequestID(t, "req-usage")

	inputTokens := 120
	outputTokens := 9
	cacheReadTokens := 70
	cacheWriteTokens := 5
	usage, err := runtimeevidence.NewTokenUsageWithOptional(&inputTokens, &outputTokens, &cacheReadTokens, &cacheWriteTokens)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}

	store.Append(context.Background(), mustTerminalEventWithUsage(t, req, "alpha", "backend-a", "gpt-4.1", runtimeevidence.ResultClassSuccess, 200, usage))

	projection := store.ProjectStatus(evidencestore.ProjectionInput{
		State:         "healthy",
		EndpointCount: 1,
		Scope:         evidencestore.ProjectionScope{Kind: evidencestore.ProjectionScopeAll},
	})
	if len(projection.RecentTraffic) != 1 {
		t.Fatalf("recent traffic len = %d, want 1", len(projection.RecentTraffic))
	}
	row := projection.RecentTraffic[0]
	if row.InputTokens == nil || *row.InputTokens != 120 {
		t.Fatalf("input tokens = %#v, want 120", row.InputTokens)
	}
	if row.OutputTokens == nil || *row.OutputTokens != 9 {
		t.Fatalf("output tokens = %#v, want 9", row.OutputTokens)
	}
	if row.CacheReadTokens == nil || *row.CacheReadTokens != 70 {
		t.Fatalf("cache read tokens = %#v, want 70", row.CacheReadTokens)
	}
	if row.CacheWriteTokens == nil || *row.CacheWriteTokens != 5 {
		t.Fatalf("cache write tokens = %#v, want 5", row.CacheWriteTokens)
	}
}

func mustRequestID(t *testing.T, raw string) runtimeevidence.RequestID {
	t.Helper()

	id, err := runtimeevidence.ParseRequestID(raw)
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	return id
}

func mustInflightEvent(t *testing.T, requestID runtimeevidence.RequestID, endpoint string, providerConfigRef string, model string) runtimeevidence.TrafficEvent {
	t.Helper()

	route, err := runtimeevidence.NewRoute(providerConfigRef, model)
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	event, err := runtimeevidence.NewInflightTrafficEvent(runtimeevidence.TrafficEventInput{
		RequestID: requestID,
		Endpoint:  endpoint,
		Route:     route,
	})
	if err != nil {
		t.Fatalf("NewInflightTrafficEvent returned error: %v", err)
	}
	return event
}

func mustTerminalEvent(t *testing.T, requestID runtimeevidence.RequestID, endpoint string, providerConfigRef string, model string, result runtimeevidence.ResultClass, statusCode int) runtimeevidence.TrafficEvent {
	t.Helper()

	route, err := runtimeevidence.NewRoute(providerConfigRef, model)
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	timing, err := runtimeevidence.NewTiming(10, 25)
	if err != nil {
		t.Fatalf("NewTiming returned error: %v", err)
	}
	event, err := runtimeevidence.NewTerminalTrafficEvent(runtimeevidence.TrafficEventInput{
		RequestID:  requestID,
		Endpoint:   endpoint,
		Route:      route,
		Result:     result,
		StatusCode: statusCode,
		Timing:     timing,
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent returned error: %v", err)
	}
	return event
}

func mustTerminalEventWithUsage(t *testing.T, requestID runtimeevidence.RequestID, endpoint string, providerConfigRef string, model string, result runtimeevidence.ResultClass, statusCode int, usage runtimeevidence.TokenUsage) runtimeevidence.TrafficEvent {
	t.Helper()

	route, err := runtimeevidence.NewRoute(providerConfigRef, model)
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	timing, err := runtimeevidence.NewTiming(10, 25)
	if err != nil {
		t.Fatalf("NewTiming returned error: %v", err)
	}
	event, err := runtimeevidence.NewTerminalTrafficEvent(runtimeevidence.TrafficEventInput{
		RequestID:  requestID,
		Endpoint:   endpoint,
		Route:      route,
		Result:     result,
		StatusCode: statusCode,
		Timing:     timing,
		TokenUsage: usage,
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent returned error: %v", err)
	}
	return event
}
