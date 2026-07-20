package exchange

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/session"
)

type candidateSelectiveRuntime struct {
	testRuntimeResolver
	transport testProviderTransport
}

type targetMutatingRuntime struct{ candidateSelectiveRuntime }

func (r targetMutatingRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.candidateSelectiveRuntime.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	backend.Target.Model = "different-model"
	return backend, nil
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
	if target.TargetID == "decode-unavailable-a" {
		backend.Codec = decodeUnavailableCodec{Codec: backend.Codec}
	}
	return backend, nil
}

type decisionOnlyRejectCodec struct{}

func (decisionOnlyRejectCodec) Encode(provider.Request) (carrier.Document, []compat.Decision, error) {
	return carrier.Document{}, []compat.Decision{{
		Feature: compat.RequestOutputFormat,
		Outcome: compat.Reject,
		Subject: "test:unmarked-a",
	}}, canonical.UnsupportedOperation("request was rejected without a backend-local marker")
}

func (decisionOnlyRejectCodec) Decode(context.Context, provider.Request, provider.Ingress) (provider.DecodedResponse, error) {
	panic("rejected request must never reach transport")
}

type unsupportedTestCodec struct{}

func (unsupportedTestCodec) Encode(provider.Request) (carrier.Document, []compat.Decision, error) {
	return carrier.Document{}, []compat.Decision{{
		Feature: compat.RequestOutputFormat,
		Outcome: compat.Reject,
		Subject: "test:candidate-a",
	}}, provider.UnsupportedByBackend(canonical.UnsupportedOperation("candidate cannot represent requested output"))
}

func (unsupportedTestCodec) Decode(context.Context, provider.Request, provider.Ingress) (provider.DecodedResponse, error) {
	panic("incompatible candidate must never be called")
}

type decodeUnavailableCodec struct{ provider.Codec }

func (c decodeUnavailableCodec) Decode(context.Context, provider.Request, provider.Ingress) (provider.DecodedResponse, error) {
	return provider.DecodedResponse{}, provider.Unavailable(canonical.NewBackendError("decode-unavailable-a", http.StatusServiceUnavailable, "decode unavailable", ""))
}

type countingCheckpointStore struct {
	base     session.Store
	getCalls int
}

func (s *countingCheckpointStore) Get(ctx context.Context, workspace string, id canonical.SwobuResponseID) (session.Checkpoint, bool, error) {
	s.getCalls++
	return s.base.Get(ctx, workspace, id)
}

func (s *countingCheckpointStore) Put(ctx context.Context, workspace string, record session.Checkpoint) error {
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
		swobuResponseID: canonical.SwobuResponseID("swobu_ex_reducer"),
		phase:           startingPhase{},
	}
}

func reducerRuntime() runtimeBundle { return withRuntime(bufferedProviderTransport(nil)) }

func mustBeginSession(t *testing.T, request canonical.CanonicalRequest) session.ResolvedRequest {
	t.Helper()
	prepared, err := session.Begin(request)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

type activeProviderExecution struct {
	providerCallAttempt
	id   providerCallAttemptID
	call providerCall
}

func activeProviderAttempt(t *testing.T, state exchangeState) activeProviderExecution {
	t.Helper()
	phase, ok := state.phase.(callingProviderPhase)
	if !ok {
		t.Fatalf("phase = %T, want callingProviderPhase", state.phase)
	}
	attempt, ok := findProviderCallAttempt(state, phase.attemptID)
	if !ok {
		t.Fatalf("active provider call attempt %d is missing", phase.attemptID)
	}
	return activeProviderExecution{providerCallAttempt: attempt, id: phase.attemptID, call: phase.call}
}

func beginPreparedProviderCall(t *testing.T, state exchangeState) reducerOutcome {
	t.Helper()
	outcome, err := advanceProviderExecution(state, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func TestReducerStartProducesOneWireOnlyProviderCommand(t *testing.T) {
	tr, err := reduce(context.Background(), reducerTestState(t), exchangeStarted{}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	active := activeProviderAttempt(t, tr.nextState)
	cmd, ok := tr.command.(callProviderCommand)
	if !ok {
		t.Fatalf("command = %T, want callProviderCommand", tr.command)
	}
	if cmd.document.IsEmpty() || cmd.backend.Transport == nil {
		t.Fatalf("provider command is not final wire I/O: %#v", cmd)
	}
	if active.call.fullRequest.Model() == "" || cmd.attemptID != active.id {
		t.Fatal("active phase lost resolved session request")
	}
	if len(tr.nextState.providerCallAttempts) != 1 || active.status != providerCallAttemptCalling {
		t.Fatalf("provider calls = %#v, want one calling attempt before command execution", tr.nextState.providerCallAttempts)
	}
}

func TestReducerRejectsUnexpectedConcreteEvent(t *testing.T) {
	_, err := reduce(context.Background(), reducerTestState(t), checkpointLoaded{}, reducerRuntime())
	if err == nil {
		t.Fatal("expected startingPhase/checkpointLoaded invariant error")
	}
}

func TestReducerSuccessDecodesProviderIngressBeforeTerminalHandoff(t *testing.T) {
	started, err := reduce(context.Background(), reducerTestState(t), exchangeStarted{}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	active := activeProviderAttempt(t, started.nextState)
	ingress, err := active.call.backend.Transport.Send(context.Background(), active.call.document)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := reduce(context.Background(), started.nextState, providerIngressReceived{attemptID: active.id, ingress: ingress}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	terminal, ok := tr.nextState.phase.(completedPhase)
	if !ok {
		t.Fatalf("phase = %T, want completedPhase", tr.nextState.phase)
	}
	if len(tr.nextState.providerCallAttempts) != 1 || terminal.target.TargetID != active.target.TargetID {
		t.Fatalf("terminal attempt = %#v ledger=%#v", terminal, tr.nextState.providerCallAttempts)
	}
	completedAttempt, _ := findProviderCallAttempt(tr.nextState, active.id)
	if completedAttempt.status != providerCallAttemptHandoffReady {
		t.Fatalf("attempt status = %d, want handoff ready", completedAttempt.status)
	}
	if tr.command != nil {
		t.Fatalf("terminal reducerOutcome produced command %T", tr.command)
	}
}

func TestReducerAloneChoosesRouteFailoverAttempt(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "retry-a"),
		requestpathTarget(t, "retry-b"),
	}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	started := beginPreparedProviderCall(t, s)
	active := activeProviderAttempt(t, started.nextState)
	tr, err := reduce(context.Background(), started.nextState, providerCallFailed{attemptID: active.id,
		err: provider.Unavailable(canonical.NewBackendError("retry-a", 503, "unavailable", "")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	next := activeProviderAttempt(t, tr.nextState)
	cmd, ok := tr.command.(callProviderCommand)
	if !ok {
		t.Fatalf("command = %T, want callProviderCommand", tr.command)
	}
	if next.candidateIndex != 1 || cmd.backend.Target.TargetID != "retry-b" {
		t.Fatalf("retry did not advance exactly once: candidate=%d target=%q", next.candidateIndex, cmd.backend.Target.TargetID)
	}
	if active.candidateIndex == next.candidateIndex || next.id != active.id+1 || len(tr.nextState.providerCallAttempts) != 2 {
		t.Fatalf("route failover attempts = %#v", tr.nextState.providerCallAttempts)
	}
}

func TestSynchronousCompletionFailureCanAdvanceRouteBeforeClientHandoff(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "decode-unavailable-a"),
		requestpathTarget(t, "retry-b"),
	}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	runner := withRuntime(nil)
	runner.Runtime = candidateSelectiveRuntime{transport: bufferedProviderTransport(nil)}
	started, err := advanceProviderExecution(s, runner)
	if err != nil {
		t.Fatal(err)
	}
	first := activeProviderAttempt(t, started.nextState)
	ingress, err := first.call.backend.Transport.Send(context.Background(), first.call.document)
	if err != nil {
		t.Fatal(err)
	}
	fellBack, err := reduce(context.Background(), started.nextState, providerIngressReceived{attemptID: first.id, ingress: ingress}, runner)
	if err != nil {
		t.Fatal(err)
	}
	recordedFirst, _ := findProviderCallAttempt(fellBack.nextState, first.id)
	second := activeProviderAttempt(t, fellBack.nextState)
	if recordedFirst.status != providerCallAttemptFailed || recordedFirst.failure.Stage != providerCallFailureBeforeHandoff || second.id != 2 || second.candidateIndex != 1 {
		t.Fatalf("completion failover attempts = %#v", fellBack.nextState.providerCallAttempts)
	}
}

func TestReducerRetriesUnavailableNativePreviousResponseWithoutReferenceOnSameTarget(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	semantic := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("a"),
		Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "complete history")},
	})
	delta := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("a"),
		Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "current delta")},
		PreviousResponse: &canonical.ResponseRef{
			SwobuID: "swobu_resp_123",
			Responses: &canonical.ResponsesNativeRef{
				ProviderResponseID: "provider_resp_789",
				TargetID:           target.ID().String(),
				TargetVersion:      uint64(target.Version()),
			},
		},
	})
	s.prepared = &session.ResolvedRequest{Full: semantic, Delta: delta}

	started := beginPreparedProviderCall(t, s)
	active := activeProviderAttempt(t, started.nextState)
	if !nativePreviousResponseSent(active.call.request) {
		t.Fatal("initial request did not use native previous_response_id")
	}
	originalSwobuResponseID := started.nextState.swobuResponseID
	nativeIngress, err := active.call.backend.Transport.Send(context.Background(), active.call.document)
	if err != nil {
		t.Fatal(err)
	}
	nativeAccepted, err := reduce(context.Background(), started.nextState, providerIngressReceived{attemptID: active.id, ingress: nativeIngress}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if !hasDecision(nativeAccepted.evidence.decisions, compat.RequestPreviousResponseResponses, compat.Exact) {
		t.Fatalf("native acceptance evidence = %#v", nativeAccepted.evidence.decisions)
	}

	retried, err := reduce(context.Background(), started.nextState, providerCallFailed{attemptID: active.id,
		err: provider.Rejected(canonical.NewBackendError("native-a", http.StatusNotFound, "response not found", "")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	retryAttempt := activeProviderAttempt(t, retried.nextState)
	if retryAttempt.candidateIndex != active.candidateIndex || retryAttempt.call.backend.Target.TargetID != active.call.backend.Target.TargetID {
		t.Fatalf("retry without native reference changed candidate or target: candidate=%d target=%q", retryAttempt.candidateIndex, retryAttempt.call.backend.Target.TargetID)
	}
	if retried.nextState.swobuResponseID != originalSwobuResponseID {
		t.Fatalf("Swobu response ID changed: got %q want %q", retried.nextState.swobuResponseID, originalSwobuResponseID)
	}
	if _, ok := retryAttempt.call.request.Canonical.PreviousResponse(); ok {
		t.Fatal("retry without native reference retained previous response reference")
	}
	message, _ := retryAttempt.call.request.Canonical.Items()[0].Message()
	text, _ := message.Content()[0].Text()
	if got := text.Text(); got != "complete history" {
		t.Fatalf("full provider request item = %q", got)
	}
	ingress, err := retryAttempt.call.backend.Transport.Send(context.Background(), retryAttempt.call.document)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := reduce(context.Background(), retried.nextState, providerIngressReceived{attemptID: retryAttempt.id, ingress: ingress}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := completed.nextState.phase.(completedPhase); !ok {
		t.Fatalf("retry without native reference phase = %T, want completedPhase", completed.nextState.phase)
	}
	if !hasDecision(completed.evidence.decisions, compat.RequestPreviousResponseResponses, compat.Drop) ||
		!hasDecision(completed.evidence.decisions, compat.RequestPreviousResponse, compat.Exact) {
		t.Fatalf("retry without native reference evidence = %#v", completed.evidence.decisions)
	}
	terminal := completed.nextState.phase.(completedPhase)
	if len(completed.nextState.providerCallAttempts) != 2 || terminal.target.TargetID != active.target.TargetID {
		t.Fatalf("terminal provider-call ledger/target = %#v %#v", completed.nextState.providerCallAttempts, terminal)
	}
	semanticFailed, err := reduce(context.Background(), retried.nextState, providerCallFailed{attemptID: retryAttempt.id,
		err: provider.Rejected(canonical.NewBackendError("native-a", http.StatusNotFound, "still unavailable", "")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := semanticFailed.nextState.phase.(callingProviderPhase); ok {
		t.Fatal("failed retry without native reference produced a third request")
	}
	if hasPreviousResponseDecision(semanticFailed.evidence.decisions) {
		t.Fatalf("terminal call failures invented previous-response evidence: %#v", semanticFailed.evidence.decisions)
	}
	if len(semanticFailed.nextState.providerCallAttempts) != 2 {
		t.Fatalf("provider calls = %d, want exactly two", len(semanticFailed.nextState.providerCallAttempts))
	}
}

func TestReducerRetriesUnstructured400WithoutNativeReference(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	s.prepared = &session.ResolvedRequest{
		Full: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("a"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "complete history")},
		}),
		Delta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("a"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "current delta")},
			PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_resp_123", Responses: &canonical.ResponsesNativeRef{
				ProviderResponseID: "provider_resp_789", TargetID: target.ID().String(), TargetVersion: uint64(target.Version()),
			}},
		}),
	}

	started := beginPreparedProviderCall(t, s)
	active := activeProviderAttempt(t, started.nextState)
	retried, err := reduce(context.Background(), started.nextState, providerCallFailed{attemptID: active.id,
		err: provider.Rejected(canonical.NewBackendError("native-a", http.StatusBadRequest, "unstructured endpoint error", "")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	retryAttempt := activeProviderAttempt(t, retried.nextState)
	if retryAttempt.candidateIndex != 0 || nativePreviousResponseSent(retryAttempt.call.request) {
		t.Fatalf("400 retry candidate=%d request=%#v", retryAttempt.candidateIndex, retryAttempt.call.request)
	}
}

func TestFailedFullHistoryCallResumesConfiguredRouteFailover(t *testing.T) {
	s := reducerTestState(t)
	nativeTarget := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{
		nativeTarget,
		requestpathTarget(t, "retry-b"),
	}}
	s.prepared = &session.ResolvedRequest{
		Full: s.input.request,
		Delta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("a"),
			PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_resp_123", Responses: &canonical.ResponsesNativeRef{
				ProviderResponseID: "provider_resp_789", TargetID: nativeTarget.ID().String(), TargetVersion: uint64(nativeTarget.Version()),
			}},
		}),
	}
	started := beginPreparedProviderCall(t, s)
	first := activeProviderAttempt(t, started.nextState)
	fullHistory, err := reduce(context.Background(), started.nextState, providerCallFailed{
		attemptID: first.id,
		err:       provider.Rejected(canonical.NewBackendError("native-a", http.StatusNotFound, "missing", "")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	second := activeProviderAttempt(t, fullHistory.nextState)
	routeFailover, err := reduce(context.Background(), fullHistory.nextState, providerCallFailed{
		attemptID: second.id,
		err:       provider.Unavailable(canonical.NewBackendError("native-a", http.StatusServiceUnavailable, "unavailable", "")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if hasDecision(routeFailover.evidence.decisions, compat.RequestPreviousResponseResponses, compat.Reject) {
		t.Fatalf("semantic operational failure manufactured native rejection evidence: %#v", routeFailover.evidence.decisions)
	}
	third := activeProviderAttempt(t, routeFailover.nextState)
	if len(routeFailover.nextState.providerCallAttempts) != 3 || third.id != 3 || third.candidateIndex != 1 {
		t.Fatalf("route failover after full-history failure = %#v", routeFailover.nextState.providerCallAttempts)
	}
}

func hasDecision(decisions []compat.Decision, feature compat.Feature, outcome compat.Outcome) bool {
	for _, decision := range decisions {
		if decision.Feature == feature && decision.Outcome == outcome {
			return true
		}
	}
	return false
}

func hasPreviousResponseDecision(decisions []compat.Decision) bool {
	for _, decision := range decisions {
		if decision.Feature == compat.RequestPreviousResponseResponses || decision.Feature == compat.RequestPreviousResponse {
			return true
		}
	}
	return false
}

func decisionSubject(decisions []compat.Decision, feature compat.Feature, outcome compat.Outcome) (compat.Subject, bool) {
	for _, decision := range decisions {
		if decision.Feature == feature && decision.Outcome == outcome {
			return decision.Subject, true
		}
	}
	return "", false
}

func TestReducerDoesNotRetryNativePreviousResponseWithoutReferenceOn500(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	s.prepared = &session.ResolvedRequest{
		Full: s.input.request,
		Delta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("a"),
			PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_resp_123", Responses: &canonical.ResponsesNativeRef{
				ProviderResponseID: "provider_resp_789", TargetID: target.ID().String(), TargetVersion: uint64(target.Version()),
			}},
		}),
	}
	started := beginPreparedProviderCall(t, s)
	active := activeProviderAttempt(t, started.nextState)
	failed, err := reduce(context.Background(), started.nextState, providerCallFailed{attemptID: active.id,
		err: provider.Unavailable(canonical.NewBackendError("native-a", http.StatusInternalServerError, "failed", "")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := failed.nextState.phase.(callingProviderPhase); ok {
		t.Fatal("500 triggered retry without native reference")
	}
	if hasPreviousResponseDecision(failed.evidence.decisions) {
		t.Fatalf("500 emitted previous-response compatibility evidence: %#v", failed.evidence.decisions)
	}
}

func TestTargetMismatchSelectsSemanticAndEmitsDropEvidence(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	s.prepared = &session.ResolvedRequest{
		Full: s.input.request,
		Delta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("a"),
			PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_resp_123", Responses: &canonical.ResponsesNativeRef{
				ProviderResponseID: "provider_resp_789", TargetID: target.ID().String(), TargetVersion: uint64(target.Version()) + 1,
			}},
		}),
	}
	started := beginPreparedProviderCall(t, s)
	attempt := activeProviderAttempt(t, started.nextState)
	if nativePreviousResponseSent(attempt.call.request) {
		t.Fatal("target mismatch sent native previous response")
	}
	if hasDecision(started.evidence.decisions, compat.RequestPreviousResponseResponses, compat.Drop) ||
		hasDecision(started.evidence.decisions, compat.RequestPreviousResponse, compat.Exact) {
		t.Fatalf("target mismatch emitted success evidence before handoff: %#v", started.evidence.decisions)
	}
	ingress, err := attempt.call.backend.Transport.Send(context.Background(), attempt.call.document)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := reduce(context.Background(), started.nextState, providerIngressReceived{attemptID: attempt.id, ingress: ingress}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if !hasDecision(completed.evidence.decisions, compat.RequestPreviousResponseResponses, compat.Drop) ||
		!hasDecision(completed.evidence.decisions, compat.RequestPreviousResponse, compat.Exact) {
		t.Fatalf("target mismatch handoff evidence = %#v", completed.evidence.decisions)
	}
}

func TestNativeRequestDecodeFailureDoesNotTriggerSemanticRetry(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	s.prepared = &session.ResolvedRequest{
		Full: s.input.request,
		Delta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("a"),
			PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_resp_123", Responses: &canonical.ResponsesNativeRef{
				ProviderResponseID: "provider_resp_789", TargetID: target.ID().String(), TargetVersion: uint64(target.Version()),
			}},
		}),
	}
	started := beginPreparedProviderCall(t, s)
	active := activeProviderAttempt(t, started.nextState)
	failed, err := reduce(context.Background(), started.nextState, providerIngressReceived{
		attemptID: active.id,
		ingress:   provider.DocumentIngress{},
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := failed.nextState.phase.(callingProviderPhase); ok {
		t.Fatal("provider decode/partial-output failure triggered retry without native reference")
	}
	failedAttempt, _ := findProviderCallAttempt(failed.nextState, active.id)
	if failedAttempt.status != providerCallAttemptFailed || failedAttempt.failure.Stage != providerCallFailureBeforeHandoff {
		t.Fatalf("attempt = %#v, want failed before handoff", failedAttempt)
	}
	if hasPreviousResponseDecision(failed.evidence.decisions) {
		t.Fatalf("decode failure emitted previous-response compatibility evidence: %#v", failed.evidence.decisions)
	}
}

func TestProviderResultAttemptIdentityRejectsUnknownAndNonActiveCallingAttempts(t *testing.T) {
	state := reducerTestState(t)
	if _, err := reduce(context.Background(), state, providerCallFailed{attemptID: 99, err: errors.New("unknown")}, reducerRuntime()); err == nil {
		t.Fatal("unknown provider call attempt was accepted")
	}
	state.providerCallAttempts = []providerCallAttempt{
		{status: providerCallAttemptCalling},
		{status: providerCallAttemptCalling},
	}
	state.phase = callingProviderPhase{attemptID: 2}
	if _, err := reduce(context.Background(), state, providerCallFailed{attemptID: 1, err: errors.New("stale")}, reducerRuntime()); err == nil {
		t.Fatal("non-active calling provider call attempt was accepted")
	}
}

func TestDuplicateProviderResultForTerminalAttemptIsInvariantError(t *testing.T) {
	started, err := reduce(context.Background(), reducerTestState(t), exchangeStarted{}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	active := activeProviderAttempt(t, started.nextState)
	failed, err := reduce(context.Background(), started.nextState, providerCallFailed{
		attemptID: active.id,
		err:       provider.Rejected(canonical.NewBackendError("a", http.StatusConflict, "rejected", "")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reduce(context.Background(), failed.nextState, providerCallFailed{attemptID: active.id, err: errors.New("duplicate")}, reducerRuntime()); err == nil {
		t.Fatal("duplicate result was accepted by the synchronous reducer contract")
	}
}

func TestProviderCallAttemptHistoryRetainsFactsNotExecutableCall(t *testing.T) {
	typeOfAttempt := reflect.TypeOf(providerCallAttempt{})
	for _, forbidden := range []string{"call", "backend", "document", "clientCodec"} {
		if _, ok := typeOfAttempt.FieldByName(forbidden); ok {
			t.Fatalf("providerCallAttempt retains executable field %q", forbidden)
		}
	}
	for _, forbidden := range []string{"form", "id", "candidate"} {
		if _, ok := typeOfAttempt.FieldByName(forbidden); ok {
			t.Fatalf("providerCallAttempt retains derivable field %q", forbidden)
		}
	}
	if _, ok := reflect.TypeOf(routePlan{}).FieldByName("index"); ok {
		t.Fatal("routePlan retains a cursor alongside attempt candidate indices")
	}
}

func TestProviderCallAttemptLifecycleRejectsIllegalTransitions(t *testing.T) {
	state := exchangeState{providerCallAttempts: []providerCallAttempt{{status: providerCallAttemptCalling}}}
	acceptedState, err := completeProviderCallAttempt(state, 1)
	if err != nil {
		t.Fatal(err)
	}
	if acceptedState.providerCallAttempts[0].status != providerCallAttemptHandoffReady {
		t.Fatalf("status = %d, want handoff ready", acceptedState.providerCallAttempts[0].status)
	}
	if _, err := completeProviderCallAttempt(acceptedState, 1); err == nil {
		t.Fatal("terminal attempt completed twice")
	}
	if _, err := failProviderCallAttempt(state, 1, providerCallFailureBeforeIngress, nil); err == nil {
		t.Fatal("failed status accepted a nil failure")
	}
}

func TestPreparationFailureDoesNotAllocateProviderCallAttempt(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{requestpathTarget(t, "unmarked-a")}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	runner := withRuntime(nil)
	runner.Runtime = candidateSelectiveRuntime{transport: bufferedProviderTransport(nil)}

	outcome, err := advanceProviderExecution(s, runner)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := outcome.nextState.phase.(failedPhase)
	if !ok {
		t.Fatalf("phase = %T, want failedPhase", outcome.nextState.phase)
	}
	if len(outcome.nextState.providerCallAttempts) != 0 || outcome.command != nil {
		t.Fatalf("local preparation created provider execution: %#v", outcome)
	}
}

func TestFallbackEligiblePreparationFailureSkipsCandidateWithoutConsumingAttemptID(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "incompatible-a"),
		requestpathTarget(t, "compatible-b"),
	}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	runner := withRuntime(nil)
	runner.Runtime = candidateSelectiveRuntime{transport: bufferedProviderTransport(nil)}

	outcome, err := advanceProviderExecution(s, runner)
	if err != nil {
		t.Fatal(err)
	}
	attempt := activeProviderAttempt(t, outcome.nextState)
	if len(outcome.nextState.providerCallAttempts) != 1 || attempt.id != 1 || attempt.candidateIndex != 1 {
		t.Fatalf("provider execution after local rejection = %#v", outcome.nextState)
	}
}

func TestCandidateScopedAsyncPreparationFailureAdvancesRoute(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "media-a"),
		requestpathTarget(t, "media-b"),
	}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	s.phase = preparingProviderAttemptPhase{
		selection: providerCallSelection{candidateIndex: 0, requestChoice: providerRequestPreferred},
		target:    provider.TargetSnapshot{TargetID: "media-a"},
	}
	runner := withRuntime(nil)
	runner.Runtime = candidateSelectiveRuntime{transport: bufferedProviderTransport(nil)}
	outcome, err := reduce(context.Background(), s, providerAttemptPrepared{
		selection: providerCallSelection{candidateIndex: 0, requestChoice: providerRequestPreferred},
		err:       preparationError(PreparationCandidate, "candidate protocol cannot preserve image placement"),
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	attempt := activeProviderAttempt(t, outcome.nextState)
	if attempt.candidateIndex != 1 || attempt.target.TargetID != "media-b" {
		t.Fatalf("candidate fallback = %#v", attempt.providerCallAttempt)
	}
}

func TestBackendResolutionMustPreserveResolvedTargetProjection(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{requestpathTarget(t, "target-a")}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	runner := withRuntime(nil)
	runner.Runtime = targetMutatingRuntime{candidateSelectiveRuntime{transport: bufferedProviderTransport(nil)}}

	outcome, err := advanceProviderExecution(s, runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outcome.nextState.phase.(failedPhase); !ok {
		t.Fatalf("phase = %T, want failedPhase", outcome.nextState.phase)
	}
	if len(outcome.nextState.providerCallAttempts) != 0 {
		t.Fatalf("changed target projection issued attempt: %#v", outcome.nextState.providerCallAttempts)
	}
}

func TestProviderCallRequirementsAndEvidenceSubjectUseActualAttempt(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	s.prepared = &session.ResolvedRequest{
		Full: s.input.request,
		Delta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("a"),
			PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_resp_123", Responses: &canonical.ResponsesNativeRef{
				ProviderResponseID: "provider_resp_789", TargetID: target.ID().String(), TargetVersion: uint64(target.Version()),
			}},
		}),
	}
	started := beginPreparedProviderCall(t, s)
	attempt := activeProviderAttempt(t, started.nextState)
	requirementsFunction := reflect.TypeOf(providerCallRequirements)
	if requirementsFunction.In(0) != reflect.TypeOf(providerCall{}) {
		t.Fatalf("providerCallRequirements input = %v, want complete providerCall", requirementsFunction.In(0))
	}
	requirements := providerCallRequirements(attempt.call)
	if len(requirements) != 1 || requirements[0] != compat.RequestPreviousResponseResponses {
		t.Fatalf("provider call requirements = %#v", requirements)
	}
	wantSubject := compat.Subject(fmt.Sprintf("route:target/native-a/version/%d/provider_call/1", target.Version()))
	if got := previousResponseDecision(attempt.providerCallAttempt, attempt.id, compat.Exact).Subject; got != wantSubject {
		t.Fatalf("subject = %q, want %q", got, wantSubject)
	}
}

func TestUnrelatedUnsupportedFailureDoesNotBecomeNativeObservationOrRetry(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	s.prepared = &session.ResolvedRequest{
		Full: s.input.request,
		Delta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("a"), PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_resp_123", Responses: &canonical.ResponsesNativeRef{
				ProviderResponseID: "provider_resp_789", TargetID: target.ID().String(), TargetVersion: uint64(target.Version()),
			}},
		}),
	}
	started := beginPreparedProviderCall(t, s)
	active := activeProviderAttempt(t, started.nextState)
	failed, err := reduce(context.Background(), started.nextState, providerCallFailed{
		attemptID: active.id,
		err:       provider.UnsupportedByBackend(canonical.UnsupportedOperation("tool choice is unsupported")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := failed.nextState.phase.(callingProviderPhase); ok {
		t.Fatal("unrelated unsupported failure selected semantic history")
	}
	if hasPreviousResponseDecision(failed.evidence.decisions) {
		t.Fatalf("unrelated unsupported failure emitted native evidence: %#v", failed.evidence.decisions)
	}
}

type unsupportedTestCommand struct{}

func (unsupportedTestCommand) isCommand() {}

func TestExecuteCommandPanicsForUnsupportedClosedCommand(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unsupported closed command did not panic")
		}
	}()
	_ = executeCommand(context.Background(), unsupportedTestCommand{})
}

func TestBackendRejectionStatusesDoNotAdvanceRoute(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			err := canonical.NewBackendError("rejected-a", status, "request rejected", "")
			if routeFailoverEligible(err) {
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

func TestExchangeLoadsCheckpointOnceAcrossProviderFallback(t *testing.T) {
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
	store := &countingCheckpointStore{base: session.NewMemoryStore()}
	previousResponse, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "resp_previous"}, "a", []canonical.CanonicalItem{
		testMessage(canonical.MessageRoleAssistant, "answer one"),
	}, "stop", canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	previous := session.Checkpoint{
		Request:  canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("a"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "turn one")}}),
		Response: previousResponse,
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
	runner.CheckpointStore = store
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("a"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "turn two")}, PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})

	_, err = runExchange(context.Background(), runner, "ex_fallback", "unknown", canonical.ClientFamilyResponses, delivery.BufferedDelivery(), request, workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	if store.getCalls != 1 {
		t.Fatalf("checkpoint Get calls = %d, want exactly 1 across fallback", store.getCalls)
	}
	if providerCalls != 2 {
		t.Fatalf("provider calls = %d, want 2", providerCalls)
	}
}
