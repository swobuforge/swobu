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
	"github.com/swobuforge/swobu/internal/turnstate"
)

// TestExchangeMachine_RetryFallback_SelectsSecondBackendOnFirstFailure proves
// the machine retries the next target in the plan when the first returns a
// retryable backend error.
func TestExchangeMachine_RetryFallback_SelectsSecondBackendOnFirstFailure(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls []string
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		if len(calls) == 1 {
			return nil, canonical.NewBackendError(req.Target.BackendRef, http.StatusServiceUnavailable, "down", "")
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

// ---- continuation (adversarial) -------------------------------------------------
//
// These tests prove that the continuation pipeline:
//   1. Prepares continuation per-attempt (materializes history on fallback)
//   2. Preserves native continuation for responses-native targets
//   3. Fails closed on unsafe native replay divergence
//   4. Captures the completed record exactly once per successful response
//   5. Passes through cleanly when no store is wired

// TestContinuation_NilStore_Passthrough proves the pipeline completes
// successfully when no continuation store is wired.
func TestContinuation_NilStore_Passthrough(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	var capturedReq canonical.CanonicalRequest
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		capturedReq = req.Request
		return carrier.NewWireDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"resp_ok","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	})

	ingress := RequestIngress{runner: runner}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "nil-store-1",
		Request:         newTransportRequestWithTurn(http.MethodPost, "/responses", "nonexistent", map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "hello"}}}),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("nil store should passthrough: %v", err)
	}
	if capturedReq.Turn().IsZero() {
		t.Fatal("nil store should preserve turn reference (fail-open for 400 downstream)")
	}
}

// TestContinuation_NativeContinuation_Preserved proves that for a
// responses-native target the turn reference is kept when the current
// request is a strict super-set of the stored chain.
func TestContinuation_NativeContinuation_Preserved(t *testing.T) {
	store := turnstate.NewMemoryContinuationStore()
	prevID := canonical.NewContinuationID("resp_prev")
	_ = store.Put(context.Background(), canonical.ContinuationRecord{
		ID:      prevID,
		RouteID: "alpha",
		ModelID: "m",
		RequestDelta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "first-turn"),
			},
		}),
		// Minimal stored response so chain length is 1 user item.
		// Current thread [user:first-turn, assistant:reply, user:second-turn]
		// is a strict superset (prefixLen == 1 == len(anchor)).
		Response: canonical.NewOutputWithUsage(
			canonical.SemanticKindConversation,
			"resp_prev",
			"m",
			[]canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant-reply")},
			"stop",
			canonical.TokenUsage{},
		),
		Status: canonical.ContinuationStatusCompleted,
	})

	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	var capturedReq canonical.CanonicalRequest
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		capturedReq = req.Request
		return carrier.NewWireDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"native-ok","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	})

	ingress := RequestIngress{runner: runner.WithContinuationStore(store)}

	// Exact match to stored chain + one new user item = strict superset -> native turn kept.
	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "native-1",
		Request:         newTransportRequestWithTurn(http.MethodPost, "/responses", "resp_prev", map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "first-turn"}, {"role": "assistant", "content": "assistant-reply"}, {"role": "user", "content": "second-turn"}}}),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedReq.Turn().IsZero() {
		t.Fatal("native continuation: turn should be preserved when current thread is superset of chain")
	}

	// Continuation record should be captured for the new response.
	rec, ok, err := store.Get(context.Background(), canonical.NewContinuationID("native-1_result"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("continuation record was not captured")
	}
	if rec.Status != canonical.ContinuationStatusCompleted {
		t.Fatalf("record status = %q, want completed", rec.Status)
	}
}

// TestContinuation_Fallback_MaterializesHistory proves that when the first
// backend fails and the exchange falls back to a second backend with a
// different protocol, the stored continuation history is materialized into the
// outgoing request.
func TestContinuation_Fallback_MaterializesHistory(t *testing.T) {
	store := turnstate.NewMemoryContinuationStore()
	prevID := canonical.NewContinuationID("resp_prev")
	_ = store.Put(context.Background(), canonical.ContinuationRecord{
		ID:      prevID,
		RouteID: "alpha",
		ModelID: "m",
		RequestDelta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "previous-turn"),
			},
		}),
		Response: canonical.NewOutputWithUsage(
			canonical.SemanticKindConversation,
			"resp_prev",
			"m",
			[]canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "prev-response")},
			"stop",
			canonical.TokenUsage{},
		),
		Status: canonical.ContinuationStatusCompleted,
	})

	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls []string
	var capturedReqs []canonical.CanonicalRequest
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		capturedReqs = append(capturedReqs, req.Request)
		if len(calls) == 1 {
			return nil, canonical.NewBackendError(req.Target.BackendRef, http.StatusServiceUnavailable, "down", "")
		}
		return carrier.NewWireDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"fallback-ok","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	})

	ingress := RequestIngress{runner: runner.WithContinuationStore(store)}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "fallback-1",
		Request:         newTransportRequestWithTurn(http.MethodPost, "/responses", "resp_prev", map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "current-turn"}}}),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(calls))
	}
	if calls[0] != "backend-a" || calls[1] != "backend-b" {
		t.Fatalf("unexpected call order: %v", calls)
	}

	// Second attempt materialized history = previous-turn + assistant-reply + current-turn.
	secondReq := capturedReqs[1]
	items := secondReq.Items()
	if len(items) < 2 {
		t.Fatalf("fallback: expected >=2 items, got %d", len(items))
	}
	found := map[string]bool{}
	for _, it := range items {
		found[it.Text] = true
	}
	if !found["previous-turn"] {
		t.Fatal("fallback: missing previous-turn")
	}
	if !found["current-turn"] {
		t.Fatal("fallback: missing current-turn")
	}

	// New record captured.
	rec, ok, err := store.Get(context.Background(), canonical.NewContinuationID("fallback-1_result"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("continuation record was not captured after fallback success")
	}
	if rec.RouteID != "alpha" {
		t.Fatalf("record routeID = %q, want alpha", rec.RouteID)
	}
}

// TestContinuation_UnsafeNativeReplay_FailsClosed proves that when a
// Responses target is selected but the current request diverges from the
// stored chain, the router emits a 400-style error (UnsafeNativeReplayError)
// instead of silently materializing a divergent thread.
func TestContinuation_UnsafeNativeReplay_FailsClosed(t *testing.T) {
	store := turnstate.NewMemoryContinuationStore()
	prevID := canonical.NewContinuationID("resp_prev")
	_ = store.Put(context.Background(), canonical.ContinuationRecord{
		ID:      prevID,
		RouteID: "alpha",
		ModelID: "m",
		RequestDelta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "stored-turn"),
			},
		}),
		Response: canonical.NewOutputWithUsage(
			canonical.SemanticKindConversation,
			"resp_prev",
			"m",
			[]canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "stored-response")},
			"stop",
			canonical.TokenUsage{},
		),
		Status: canonical.ContinuationStatusCompleted,
	})

	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	var providerCalled bool
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		providerCalled = true
		return nil, canonical.InternalError("should not reach provider")
	})

	ingress := RequestIngress{runner: runner.WithContinuationStore(store)}

	// Divergent request: overlapping but not prefix-equal to stored chain.
	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "unsafe-replay-1",
		Request:         newTransportRequestWithTurn(http.MethodPost, "/responses", "resp_prev", map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "stored-turn"}, {"role": "user", "content": "divergent-turn"}}}),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if providerCalled {
		t.Fatal("provider should not be called for unsafe native replay")
	}
	if err == nil {
		t.Fatal("expected error for unsafe native replay, got nil")
	}
	// UnsafeNativeReplayError unwraps to a BadRequest.
	if !strings.Contains(err.Error(), "400") && !strings.Contains(strings.ToLower(err.Error()), "unsafe") {
		t.Fatalf("expected UnsafeNativeReplayError (400), got: %v", err)
	}
}

// TestContinuation_NoDuplicate_PersistsOnceOnRetry proves that even when
// multiple fallback attempts occur, the continuation record is only persisted
// once — on the first successful response.
func TestContinuation_NoDuplicate_PersistsOnceOnRetry(t *testing.T) {
	store := turnstate.NewMemoryContinuationStore()

	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls int
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls++
		if calls == 1 {
			return nil, canonical.NewBackendError("backend-a", http.StatusServiceUnavailable, "down", "")
		}
		return carrier.NewWireDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"retry-ok","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	})

	ingress := RequestIngress{runner: runner.WithContinuationStore(store)}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "no-dup-1",
		Request:         newTransportRequestWithTurn(http.MethodPost, "/responses", "", map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "hi"}}}),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if calls != 2 {
		t.Fatalf("expected 2 provider calls, got %d", calls)
	}

	rec, ok, err := store.Get(context.Background(), canonical.NewContinuationID("no-dup-1_result"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected continuation record to be persisted")
	}
	if rec.Status != canonical.ContinuationStatusCompleted {
		t.Fatalf("record status = %q, want completed", rec.Status)
	}

	// Chain should contain only the single new record (no duplicates).
	chain, err := store.Chain(context.Background(), canonical.NewContinuationID("no-dup-1_result"))
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 1 {
		t.Fatalf("expected chain length 1, got %d", len(chain))
	}
}
