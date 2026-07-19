package exchange

import (
	"context"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/routing"
)

type candidateSelectiveRuntime struct {
	testRuntimeResolver
	transport testProviderTransport
}

func (r candidateSelectiveRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := newTestBackend(target, r.transport)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.TargetID == "incompatible-a" {
		backend.Codec = unsupportedTestCodec{}
	}
	if target.TargetID == "unmarked-a" {
		backend.Codec = decisionOnlyRejectCodec{}
	}
	return backend, nil
}

type decisionOnlyRejectCodec struct{}

func (decisionOnlyRejectCodec) Encode(provider.Request) (carrier.Document, []compat.Decision, error) {
	return carrier.Document{}, []compat.Decision{{
		Feature: compat.RequestStructuredOutput,
		Outcome: compat.Reject,
		Subject: "test:unmarked-a",
	}}, canonical.UnsupportedOperation("request was rejected without a backend-local marker")
}

func (decisionOnlyRejectCodec) Decode(context.Context, string, provider.Ingress) (provider.DecodedResponse, error) {
	panic("rejected request must never reach transport")
}

type unsupportedTestCodec struct{}

func (unsupportedTestCodec) Encode(provider.Request) (carrier.Document, []compat.Decision, error) {
	return carrier.Document{}, []compat.Decision{{
		Feature: compat.RequestStructuredOutput,
		Outcome: compat.Reject,
		Subject: "test:candidate-a",
	}}, provider.UnsupportedByBackend(canonical.UnsupportedOperation("candidate cannot represent requested output"))
}

func (unsupportedTestCodec) Decode(context.Context, string, provider.Ingress) (provider.DecodedResponse, error) {
	panic("incompatible candidate must never be called")
}

type countingReplayStore struct {
	base     replay.Store
	getCalls int
}

func (s *countingReplayStore) Get(ctx context.Context, workspace string, id replay.ID) (replay.Record, bool, error) {
	s.getCalls++
	return s.base.Get(ctx, workspace, id)
}

func (s *countingReplayStore) Put(ctx context.Context, workspace string, record replay.Record) error {
	return s.base.Put(ctx, workspace, record)
}

func reducerTestState(t *testing.T) exchangeState {
	t.Helper()
	return exchangeState{
		input: exchangeInput{
			exchangeID:     "ex_reducer",
			clientFamily:   canonical.ClientFamilyResponses,
			clientDelivery: delivery.BufferedDelivery(),
			request:        testCanonicalRequest("a"),
			workspace:      requestpathWorkspace(t),
		},
		responseID: replay.ResponseID("swobu_ex_reducer"),
		phase:      startingPhase{},
	}
}

func reducerRuntime() runtimeBundle { return withRuntime(bufferedProviderTransport(nil)) }

func TestReducerStartProducesOneWireOnlyProviderCommand(t *testing.T) {
	tr, err := reduce(context.Background(), reducerTestState(t), exchangeStarted{}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	active, ok := tr.nextState.phase.(callingProviderPhase)
	if !ok {
		t.Fatalf("phase = %T, want callingProviderPhase", tr.nextState.phase)
	}
	cmd, ok := tr.command.(callProviderCommand)
	if !ok {
		t.Fatalf("command = %T, want callProviderCommand", tr.command)
	}
	if cmd.document.IsEmpty() || cmd.backend.Transport == nil {
		t.Fatalf("provider command is not final wire I/O: %#v", cmd)
	}
	if active.call.semanticRequest.Model() == "" {
		t.Fatal("active phase lost immutable replay preparation")
	}
}

func TestReducerRejectsUnexpectedConcreteEvent(t *testing.T) {
	_, err := reduce(context.Background(), reducerTestState(t), providerReturned{}, reducerRuntime())
	if err == nil {
		t.Fatal("expected startingPhase/providerReturned invariant error")
	}
}

func TestReducerSuccessDecodesProviderIngressBeforeTerminalHandoff(t *testing.T) {
	started, err := reduce(context.Background(), reducerTestState(t), exchangeStarted{}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	active := started.nextState.phase.(callingProviderPhase)
	ingress, err := active.call.backend.Transport.Send(context.Background(), active.call.document)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := reduce(context.Background(), started.nextState, providerReturned{ingress: ingress}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	terminal, ok := tr.nextState.phase.(responseReturnedPhase)
	if !ok {
		t.Fatalf("phase = %T, want responseReturnedPhase", tr.nextState.phase)
	}
	if terminal.count != 1 || terminal.attempt.Index != active.attempt.Index {
		t.Fatalf("terminal attempt = %#v", terminal)
	}
	if tr.command != nil {
		t.Fatalf("terminal reducerOutcome produced command %T", tr.command)
	}
}

func TestReducerAloneChoosesRetryAttempt(t *testing.T) {
	s := reducerTestState(t)
	s.route = newRouteCursor([]routing.Attempt{
		{Index: 0, Target: requestpathTarget(t, "retry-a")},
		{Index: 1, Target: requestpathTarget(t, "retry-b")},
	})
	prepared := replay.PrepareCurrent(s.input.request)
	s.prepared = &prepared
	started, err := beginProviderCall(s, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	active := started.nextState.phase.(callingProviderPhase)
	tr, err := reduce(context.Background(), started.nextState, providerReturned{
		err: provider.Unavailable(canonical.NewBackendError("retry-a", 503, "unavailable", "")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	next, ok := tr.nextState.phase.(callingProviderPhase)
	if !ok {
		t.Fatalf("phase = %T, want callingProviderPhase", tr.nextState.phase)
	}
	cmd, ok := tr.command.(callProviderCommand)
	if !ok {
		t.Fatalf("command = %T, want callProviderCommand", tr.command)
	}
	if tr.nextState.route.index != 1 || next.attempt.Index != 1 || cmd.backend.Target.TargetID != "retry-b" {
		t.Fatalf("retry did not advance exactly once: route=%d phase=%d target=%q", tr.nextState.route.index, next.attempt.Index, cmd.backend.Target.TargetID)
	}
	if active.attempt.Index == next.attempt.Index {
		t.Fatal("retry reused the prior attempt")
	}
}

func TestBackendRejectionStatusesDoNotAdvanceRoute(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			err := canonical.NewBackendError("rejected-a", status, "request rejected", "")
			if fallbackEligibleFailure(err) {
				t.Fatalf("status %d was treated as fallback-eligible", status)
			}
		})
	}
}

func TestBackendLocalUnsupportedAdvancesRoute(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routeName, _ := routing.ParseRouteName("a")
	firstTier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "incompatible-a")})
	secondTier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "compatible-b")})
	route, _ := routing.NewRoute(routeName, []routing.Tier{firstTier, secondTier})
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}

	providerCalls := 0
	runner := withRuntime(nil)
	runner.Runtime = candidateSelectiveRuntime{transport: func(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
		providerCalls++
		return bufferedProviderTransport(nil)(ctx, target, doc)
	}}

	_, err = runExchange(context.Background(), runner, "ex_codec_fallback", "unknown", canonical.ClientFamilyResponses, delivery.BufferedDelivery(), testCanonicalRequest("a"), workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	if providerCalls != 1 {
		t.Fatalf("provider calls = %d, want only compatible candidate B", providerCalls)
	}
}

func TestCompatibilityRejectDoesNotMakeFailureFallbackEligible(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routeName, _ := routing.ParseRouteName("a")
	firstTier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "unmarked-a")})
	secondTier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "must-not-run-b")})
	route, _ := routing.NewRoute(routeName, []routing.Tier{firstTier, secondTier})
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}

	providerCalls := 0
	runner := withRuntime(nil)
	runner.Runtime = candidateSelectiveRuntime{transport: func(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
		providerCalls++
		return bufferedProviderTransport(nil)(ctx, target, doc)
	}}

	_, err = runExchange(context.Background(), runner, "ex_decision_is_not_policy", "unknown", canonical.ClientFamilyResponses, delivery.BufferedDelivery(), testCanonicalRequest("a"), workspace, nil)
	if err == nil {
		t.Fatal("unmarked codec rejection unexpectedly succeeded through fallback")
	}
	if providerCalls != 0 {
		t.Fatalf("provider calls = %d, want no transport or fallback from compatibility evidence", providerCalls)
	}
}

func TestExchangeLoadsReplayOnceAcrossProviderFallback(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routeName, _ := routing.ParseRouteName("a")
	tier, _ := routing.NewTier([]routing.Target{
		requestpathTarget(t, "retry-a"),
		requestpathTarget(t, "retry-b"),
	})
	route, _ := routing.NewRoute(routeName, []routing.Tier{tier})
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	store := &countingReplayStore{base: replay.NewMemoryStore()}
	previous := replay.Record{
		ID:      "resp_previous",
		Request: canonical.NewCanonicalRequest(canonical.RequestParams{Model: "a", InputText: "turn one"}),
		Response: canonical.NewConversationOutput("resp_previous", "a", []canonical.OutputItem{
			canonical.NewTextOutputItem("text_0", "answer one"),
		}, "stop"),
	}
	if err := store.Put(context.Background(), "dev", previous); err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	runner := withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		providerCalls++
		if target.TargetID == "retry-a" {
			return nil, canonical.NewBackendError("retry-a", http.StatusServiceUnavailable, "unavailable", "")
		}
		return bufferedProviderTransport(nil)(context.Background(), target, carrier.Document{})
	})
	runner.ReplayStore = store
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "a", InputText: "turn two", Turn: canonical.NewTurnRef("resp_previous"),
	})

	_, err = runExchange(context.Background(), runner, "ex_fallback", "unknown", canonical.ClientFamilyResponses, delivery.BufferedDelivery(), request, workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	if store.getCalls != 1 {
		t.Fatalf("replay Get calls = %d, want exactly 1 across fallback", store.getCalls)
	}
	if providerCalls != 2 {
		t.Fatalf("provider calls = %d, want 2", providerCalls)
	}
}
