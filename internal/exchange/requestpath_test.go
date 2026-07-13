package exchange

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/routing"
)

// TestExchangeMachine_RetryFallback_SelectsSecondBackendOnFirstFailure proves
// the machine retries the next target in the plan when the first returns a
// retryable backend error.
func TestExchangeMachine_RetryFallback_SelectsSecondBackendOnFirstFailure(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls []string
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		if req.Target.BackendRef == "backend-a" {
			return nil, canonical.NewBackendError("backend-a", http.StatusServiceUnavailable, "down", "")
		}
		return carrier.NewWireDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"resp_2","model":"m","output_text":"fallback-ok"}`),
			carrier.Meta{},
		), nil
	})

	sink := &recordingEvidenceSink{}
	ingress := RequestIngress{trafficEvidence: sink, runner: runner}

	out, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "fallback-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"messages":[{"role":"user","content":"hi"}]}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Response.Transport.Status != http.StatusOK {
		t.Fatalf("response status = %d, want 200", out.Response.Transport.Status)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d (%v)", len(calls), calls)
	}
	if calls[0] != "backend-a" {
		t.Fatalf("first call should be backend-a, got %s", calls[0])
	}
	if calls[1] != "backend-b" {
		t.Fatalf("second call should be backend-b, got %s", calls[1])
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 terminal traffic event, got %d", len(sink.events))
	}
	if got := sink.events[0].Route().ProviderConfigRef(); got != "backend-b" {
		t.Fatalf("terminal event should report backend-b, got %s", got)
	}
	if got := sink.events[0].Result(); got != trafficevidence.ResultClassSuccess {
		t.Fatalf("terminal event result = %q, want success", got)
	}
	if got := sink.events[0].AttemptCount(); got != 2 {
		t.Fatalf("attempt count = %d, want 2", got)
	}
}

// TestExchangeMachine_NonRetryableFailure_TerminatesImmediately proves that a
// non-retryable failure (400 Bad Request) terminates after the first backend
// with no retry attempt.
func TestExchangeMachine_NonRetryableFailure_TerminatesImmediately(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls []string
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		return nil, canonical.NewBackendError(req.Target.BackendRef, http.StatusBadRequest, "bad request", "")
	})

	sink := &recordingEvidenceSink{}
	ingress := RequestIngress{trafficEvidence: sink, runner: runner}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "non-retryable-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"messages":[{"role":"user","content":"hi"}]}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err := err; err == nil {
		t.Fatal("expected error for non-retryable failure, got nil")
	}

	// Should only call one backend, then terminate immediately.
	if len(calls) != 1 {
		t.Fatalf("expected 1 provider call, got %d (%v)", len(calls), calls)
	}
	first := calls[0]

	// Evidence should record the single failed attempt.
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 terminal traffic event, got %d", len(sink.events))
	}
	if got := sink.events[0].Route().ProviderConfigRef(); got != first {
		t.Fatalf("terminal event should report %s, got %s", first, got)
	}
	if got := sink.events[0].Result(); got != trafficevidence.ResultClassBackendError {
		t.Fatalf("terminal event result = %q, want backend_error", got)
	}
	if got := sink.events[0].AttemptCount(); got != 1 {
		t.Fatalf("attempt count = %d, want 1", got)
	}
}

// TestExchangeMachine_RetryFallback_RetriesBothThenExhausts proves that when
// every backend errors, the machine tries each once and returns the last error.
func TestExchangeMachine_RetryFallback_RetriesBothThenExhausts(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls []string
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		return nil, canonical.NewBackendError(req.Target.BackendRef, http.StatusServiceUnavailable, "down", "")
	})

	sink := &recordingEvidenceSink{}
	ingress := RequestIngress{trafficEvidence: sink, runner: runner}
	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "fallback-exhaust",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"messages":[{"role":"user","content":"hi"}]}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err == nil {
		t.Fatal("expected error when plan exhausted, got nil")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d (%v)", len(calls), calls)
	}
	// The last backend error is propagated as the final error.
	if !strings.Contains(err.Error(), "backend-b") {
		t.Fatalf("expected last backend error, got: %v", err)
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 terminal traffic event, got %d", len(sink.events))
	}
	if got := sink.events[0].AttemptCount(); got != 2 {
		t.Fatalf("attempt count = %d, want 2", got)
	}
}

// TestExchangeMachine_PlanRoute_ProducesDeterministicOrder checks that plan
// building is deterministic across repeated calls with the same parameters.
func TestExchangeMachine_PlanRoute_ProducesDeterministicOrder(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	wr := endpointToWorkspaceRouting(endpoint)
	trace := &routing.Trace{ExchangeID: "test", Workspace: wr.WorkspaceSlug}
	plan := routing.BuildPlan("test", wr.WorkspaceSlug, "m", routeTargets(wr), trace)

	if len(plan) != 2 {
		t.Fatalf("expected plan length 2, got %d", len(plan))
	}
	// Determinism check: same exchangeID produces same order.
	wr2 := endpointToWorkspaceRouting(endpoint)
	plan2 := routing.BuildPlan("test", wr2.WorkspaceSlug, "m", routeTargets(wr2), trace)
	if len(plan2) != 2 {
		t.Fatalf("expected plan2 length 2, got %d", len(plan2))
	}
	if plan[0].Target.ID != plan2[0].Target.ID || plan[1].Target.ID != plan2[1].Target.ID {
		t.Fatalf("plan not deterministic: first=%v second=%v", plan, plan2)
	}
}
