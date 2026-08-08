package exchange

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/bedrock"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

type candidateSelectiveRuntime struct {
	testRuntimeResolver
	transport testProviderTransport
}

type targetMutatingRuntime struct{ candidateSelectiveRuntime }

type protocolProjectionRuntime struct {
	testRuntimeResolver
	transport testProviderTransport
}

type bedrockStructuredFallbackRuntime struct {
	testRuntimeResolver
	transport testProviderTransport
}

func (r bedrockStructuredFallbackRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	if target.ProviderSpec == string(routing.ProviderBedrock) {
		backend, err := bedrock.NewExecutor(nil).ResolveBackend(target)
		if err != nil {
			return provider.Backend{}, err
		}
		backend.Transport = provider.BindTransport(target, r.transport)
		return backend, nil
	}
	return provider.Backend{
		Target:    target,
		Codec:     protocolcodec.Codec{Protocol: target.ProtocolKind},
		Transport: provider.BindTransport(target, r.transport),
	}, nil
}

func (r protocolProjectionRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return provider.Backend{
		Target:    target,
		Codec:     protocolcodec.Codec{Protocol: target.ProtocolKind},
		Transport: provider.BindTransport(target, r.transport),
	}, nil
}

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

func (decisionOnlyRejectCodec) Encode(provider.Request) (carrier.Document, []compat.Change, error) {
	return carrier.Document{}, nil, canonical.NotImplemented("Swobu cannot implement this request projection")
}

func (decisionOnlyRejectCodec) Decode(context.Context, provider.Request, provider.Ingress) (provider.DecodedResponse, error) {
	panic("rejected request must never reach transport")
}

type unsupportedTestCodec struct{}

func (unsupportedTestCodec) Encode(provider.Request) (carrier.Document, []compat.Change, error) {
	return carrier.Document{}, nil, provider.NewIncompatibleTarget("candidate cannot represent requested output")
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

func (s *countingCheckpointStore) IsCurrentHead(ctx context.Context, workspace string, sessionID session.ClientSessionID, checkpointID canonical.SwobuResponseID) (bool, error) {
	return s.base.IsCurrentHead(ctx, workspace, sessionID, checkpointID)
}

func (s *countingCheckpointStore) ResolveHeadByHistory(ctx context.Context, workspace string, history historyfingerprint.History) (session.Checkpoint, session.HistoryResolution, error) {
	s.getCalls++
	return s.base.ResolveHeadByHistory(ctx, workspace, history)
}

func (s *countingCheckpointStore) StartSession(ctx context.Context, workspace string, record session.Checkpoint) (session.ClientSession, error) {
	return s.base.StartSession(ctx, workspace, record)
}

func (s *countingCheckpointStore) AdvanceSession(ctx context.Context, workspace string, sessionID session.ClientSessionID, expectedHead canonical.SwobuResponseID, record session.Checkpoint) error {
	return s.base.AdvanceSession(ctx, workspace, sessionID, expectedHead, record)
}

func reducerTestState(t *testing.T) exchangeState {
	t.Helper()
	return exchangeState{
		input: exchangeInput{
			exchangeID:         "ex_reducer",
			clientFamily:       canonical.ClientFamilyResponses,
			clientDelivery:     delivery.BufferedDelivery(),
			request:            testCanonicalRequest("a"),
			requestFingerprint: testHistoryRequest([]byte("reducer-request")),
			workspace:          requestpathWorkspace(t),
		},
		swobuResponseID: canonical.SwobuResponseID("swobu_ex_reducer"),
		phase:           startingPhase{},
	}
}

func reducerRuntime() runtimeBundle { return withRuntime(bufferedProviderTransport(nil)) }

func mayHaveExecuted(err error) provider.AttemptFailure {
	return provider.AttemptMayHaveExecuted(err)
}

func rejectedBeforeExecution(err error) provider.AttemptFailure {
	return provider.AttemptRejectedBeforeExecution(err)
}

func invalidContinuationReference(target string, status int, message string) error {
	return canonical.NewClassifiedBackendError(
		canonical.BackendErrorClassContinuationReferenceInvalid,
		canonical.NewBackendError(target, status, message, ""),
	)
}

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
	outcome, err := advanceProviderExecution(context.Background(), state, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func startReducerProviderCall(t *testing.T, state exchangeState) reducerOutcome {
	t.Helper()
	preparing, err := reduce(context.Background(), state, exchangeStarted{}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	command, ok := preparing.command.(prepareMCPCommand)
	if !ok {
		t.Fatalf("command = %T, want prepareMCPCommand", preparing.command)
	}
	outcome, err := reduce(
		context.Background(), preparing.nextState,
		executeCommand(context.Background(), command), reducerRuntime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func TestReducerStartProducesOneWireOnlyProviderCommand(t *testing.T) {
	tr := startReducerProviderCall(t, reducerTestState(t))
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
	started := startReducerProviderCall(t, reducerTestState(t))
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

func TestReducerRetriesOnceBeforeRouteFailover(t *testing.T) {
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
		failure: mayHaveExecuted(provider.Unavailable(canonical.NewBackendError("retry-a", 503, "unavailable", ""))),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	next := activeProviderAttempt(t, tr.nextState)
	cmd, ok := tr.command.(callProviderCommand)
	if !ok {
		t.Fatalf("command = %T, want callProviderCommand", tr.command)
	}
	if next.candidateIndex != 0 || !next.retry || cmd.backend.Target.TargetID != "retry-a" {
		t.Fatalf("retry = %#v target=%q", next.providerCallAttempt, cmd.backend.Target.TargetID)
	}
	fellBack, err := reduce(context.Background(), tr.nextState, providerCallFailed{
		attemptID: next.id,
		failure: mayHaveExecuted(provider.Unavailable(
			canonical.NewBackendError("retry-a", 503, "still unavailable", ""),
		)),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	fallback := activeProviderAttempt(t, fellBack.nextState)
	if fallback.candidateIndex != 1 || fallback.retry || fallback.id != next.id+1 {
		t.Fatalf("route fallback attempts = %#v", fellBack.nextState.providerCallAttempts)
	}
}

func TestReducerDoesNotReplayNativeEffectAfterAmbiguousDelivery(t *testing.T) {
	key, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "mail")
	if err != nil {
		t.Fatal(err)
	}
	source, err := canonical.NewMCPConnectorSource(
		"connector_mail",
		canonical.Unspecified[[]string](),
		canonical.NewMCPApprovalNever(),
		canonical.MCPLoadingEager,
		canonical.Unspecified[[]string](),
	)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := canonical.NewMCPToolSource(key, "Mail", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{namespace})
	if err != nil {
		t.Fatal(err)
	}
	declarations, err := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}

	s := reducerTestState(t)
	s.input.request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			declarations,
			testMessage(canonical.MessageRoleUser, "send the message"),
		},
	})
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "effect-a"),
		requestpathTarget(t, "effect-b"),
	}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared

	started := beginPreparedProviderCall(t, s)
	active := activeProviderAttempt(t, started.nextState)
	failed, err := reduce(context.Background(), started.nextState, providerCallFailed{
		attemptID: active.id,
		failure: mayHaveExecuted(provider.Unavailable(
			canonical.NewBackendError("effect-a", http.StatusServiceUnavailable, "response unavailable", ""),
		)),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if failed.command != nil {
		t.Fatalf("ambiguous native effect failure issued %T", failed.command)
	}
	terminal, ok := failed.nextState.phase.(failedPhase)
	if !ok {
		t.Fatalf("phase = %T, want failedPhase", failed.nextState.phase)
	}
	if !strings.Contains(terminal.problem.Error(), "automatic replay stopped") {
		t.Fatalf("terminal problem = %v", terminal.problem)
	}
	if len(failed.nextState.providerCallAttempts) != 1 {
		t.Fatalf("provider calls = %d, want one", len(failed.nextState.providerCallAttempts))
	}
}

func TestWebSearchDoesNotOverrideEligibleRouteFailover(t *testing.T) {
	tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	if err != nil {
		t.Fatal(err)
	}
	s := reducerTestState(t)
	s.input.request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tools.Declarations()...)},
	})
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "search-a"),
		requestpathTarget(t, "search-b"),
	}}
	s.providerCallAttempts = []providerCallAttempt{{
		candidateIndex: 0,
		requestChoice:  providerRequestPreferred,
		replaySafety:   providerReplaySafe,
		retry:          true,
		status:         providerCallAttemptFailed,
		failure: &providerCallFailure{
			Attempt: mayHaveExecuted(provider.Unavailable(canonical.NewBackendError("search-a", 503, "unavailable", ""))),
		},
	}}

	selection, ok := selectNextProviderCall(s)
	if !ok || selection.candidateIndex != 1 {
		t.Fatalf("web-search failover = (%#v, %t), want second target", selection, ok)
	}
}

func TestProviderReplaySafetyUsesFinalCanonicalToolOwnership(t *testing.T) {
	webTools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	webRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, webTools.Declarations()...),
			testMessage(canonical.MessageRoleUser, "search"),
		},
	})
	if safety, err := providerReplaySafetyFor(webRequest); err != nil || safety != providerReplaySafe {
		t.Fatalf("web-search safety = %d, %v", safety, err)
	}

	key, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "mail")
	source, _ := canonical.NewMCPConnectorSource(
		"connector_mail",
		canonical.Unspecified[[]string](),
		canonical.NewMCPApprovalNever(),
		canonical.MCPLoadingEager,
		canonical.Unspecified[[]string](),
	)
	namespace, _ := canonical.NewMCPToolSource(key, "Mail", source, nil)
	nativeTools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{namespace})
	nativeRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, nativeTools.Declarations()...),
			testMessage(canonical.MessageRoleUser, "send"),
		},
	})
	if safety, err := providerReplaySafetyFor(nativeRequest); err != nil || safety != providerReplayUnsafe {
		t.Fatalf("native MCP safety = %d, %v", safety, err)
	}
}

func TestProviderRecoveryStopsAfterLocalEffectRound(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "effect-a"),
		requestpathTarget(t, "effect-b"),
	}}
	s.providerCallAttempts = []providerCallAttempt{{
		candidateIndex: 0,
		requestChoice:  providerRequestFullHistory,
		providerRound:  1,
		replaySafety:   providerReplaySafe,
		status:         providerCallAttemptFailed,
		failure: &providerCallFailure{
			Attempt: mayHaveExecuted(provider.Unavailable(errors.New("post-MCP provider failure"))),
		},
	}}
	if selection, ok := selectNextProviderCall(s); ok {
		t.Fatalf("post-MCP recovery selected %#v", selection)
	}
}

func TestProviderCancellationNeverSelectsRecovery(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "cancel-a"),
		requestpathTarget(t, "cancel-b"),
	}}
	s.providerCallAttempts = []providerCallAttempt{{
		candidateIndex: 0,
		requestChoice:  providerRequestPreferred,
		replaySafety:   providerReplaySafe,
		status:         providerCallAttemptFailed,
		failure: &providerCallFailure{
			Attempt: mayHaveExecuted(provider.Cancelled(context.Canceled)),
		},
	}}
	if selection, ok := selectNextProviderCall(s); ok {
		t.Fatalf("cancelled attempt selected %#v", selection)
	}
}

// genericBackendRejection is the adapter-edge fact for an unclassified backend
// 4xx: a complete response proves exact-target rejection, not canonical
// invalidity or absence of execution. The prose is deliberately opaque evidence,
// not a recognized provider message; the reducer acts on the typed fact.
func genericBackendRejection(target string) provider.AttemptFailure {
	return mayHaveExecuted(provider.Rejected(canonical.NewBackendError(
		target, http.StatusBadRequest,
		`{"error":{"message":"opaque-unclassified-rejection-7f3a"}}`,
		"",
	)))
}

// A replay-safe backend rejection advances directly to the next route
// candidate, without the same-target retry reserved for unavailability. This is
// the ordered-route fallback the candidate count already expresses.
func TestReplaySafeBackendRejectionAdvancesNextCandidateWithoutSameTargetRetry(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "reject-a"),
		requestpathTarget(t, "reject-b"),
	}}
	s.providerCallAttempts = []providerCallAttempt{{
		candidateIndex: 0,
		requestChoice:  providerRequestPreferred,
		providerRound:  0,
		replaySafety:   providerReplaySafe,
		retry:          false,
		status:         providerCallAttemptFailed,
		failure:        &providerCallFailure{Attempt: genericBackendRejection("reject-a")},
	}}

	selection, ok := selectNextProviderCall(s)
	if !ok {
		t.Fatalf("replay-safe rejection selected nothing")
	}
	if selection.candidateIndex != 1 || selection.retry {
		t.Fatalf("replay-safe rejection selection = %#v, want next candidate without retry", selection)
	}
}

// The identical rejection must not advance when execution may have occurred and
// replay is unsafe (native MCP). Recovery stays gated by execution possibility;
// widening the advancing cause never grants replay.
func TestReplayUnsafeBackendRejectionDoesNotAdvance(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "reject-a"),
		requestpathTarget(t, "reject-b"),
	}}
	s.providerCallAttempts = []providerCallAttempt{{
		candidateIndex: 0,
		requestChoice:  providerRequestPreferred,
		providerRound:  0,
		replaySafety:   providerReplayUnsafe,
		retry:          false,
		status:         providerCallAttemptFailed,
		failure:        &providerCallFailure{Attempt: genericBackendRejection("reject-a")},
	}}

	if selection, ok := selectNextProviderCall(s); ok {
		t.Fatalf("replay-unsafe rejection selected %#v", selection)
	}
}

func TestSynchronousCompletionFailureRetriesBeforeRouteFallback(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "decode-unavailable-a"),
		requestpathTarget(t, "retry-b"),
	}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	runner := withRuntime(nil)
	runner.Runtime = candidateSelectiveRuntime{transport: bufferedProviderTransport(nil)}
	started, err := advanceProviderExecution(context.Background(), s, runner)
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
	if recordedFirst.status != providerCallAttemptFailed ||
		recordedFirst.failure.Attempt.Execution() != provider.ExecutionMayHaveOccurred ||
		second.id != 2 || second.candidateIndex != 0 || !second.retry {
		t.Fatalf("completion retry attempts = %#v", fellBack.nextState.providerCallAttempts)
	}
}

func TestReducerRetriesUnavailableNativePreviousResponseWithoutReferenceOnSameTarget(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("a"),
		Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "current delta")},
	})
	prepared := mustNativeSession(t, current, target)
	s.prepared = &prepared

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
	if changes := completedResponseChanges(t, nativeAccepted); hasPreviousResponseDecision(changes) {
		t.Fatalf("exact native continuation emitted non-exact changes: %#v", changes)
	}

	retried, err := reduce(context.Background(), started.nextState, providerCallFailed{attemptID: active.id,
		failure: rejectedBeforeExecution(provider.Rejected(invalidContinuationReference("native-a", http.StatusNotFound, "response not found"))),
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
	changes := completedResponseChanges(t, completed)
	if !hasDecision(changes, canonical.RequestPreviousResponseResponses, compat.Approximation) {
		t.Fatalf("retry without native reference changes = %#v", changes)
	}
	terminal := completed.nextState.phase.(completedPhase)
	if len(completed.nextState.providerCallAttempts) != 2 || terminal.target.TargetID != active.target.TargetID {
		t.Fatalf("terminal provider-call ledger/target = %#v %#v", completed.nextState.providerCallAttempts, terminal)
	}
	semanticFailed, err := reduce(context.Background(), retried.nextState, providerCallFailed{attemptID: retryAttempt.id,
		failure: rejectedBeforeExecution(provider.Rejected(invalidContinuationReference("native-a", http.StatusNotFound, "still unavailable"))),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := semanticFailed.nextState.phase.(callingProviderPhase); ok {
		t.Fatal("failed retry without native reference produced a third request")
	}
	if hasPreviousResponseDecision(semanticFailed.nextState.effectiveChanges) {
		t.Fatalf("terminal call failures invented previous-response changes: %#v", semanticFailed.nextState.effectiveChanges)
	}
	if len(semanticFailed.nextState.providerCallAttempts) != 2 {
		t.Fatalf("provider calls = %d, want exactly two", len(semanticFailed.nextState.providerCallAttempts))
	}
}

func TestReducerDoesNotInferNativeReferenceFailureFromUnstructured400(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	prepared := mustNativeSession(t, canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("a"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "current delta")},
	}), target)
	s.prepared = &prepared

	started := beginPreparedProviderCall(t, s)
	active := activeProviderAttempt(t, started.nextState)
	failed, err := reduce(context.Background(), started.nextState, providerCallFailed{attemptID: active.id,
		failure: mayHaveExecuted(provider.Rejected(canonical.NewBackendError("native-a", http.StatusBadRequest, "unstructured endpoint error", ""))),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if failed.command != nil {
		t.Fatalf("unstructured 400 issued %T", failed.command)
	}
	if _, ok := failed.nextState.phase.(failedPhase); !ok {
		t.Fatalf("phase = %T, want failedPhase", failed.nextState.phase)
	}
}

func TestFailedFullHistoryCallResumesConfiguredRouteFailover(t *testing.T) {
	s := reducerTestState(t)
	nativeTarget := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{
		nativeTarget,
		requestpathTarget(t, "retry-b"),
	}}
	prepared := mustNativeSession(t, s.input.request, nativeTarget)
	s.prepared = &prepared
	started := beginPreparedProviderCall(t, s)
	first := activeProviderAttempt(t, started.nextState)
	fullHistory, err := reduce(context.Background(), started.nextState, providerCallFailed{
		attemptID: first.id,
		failure:   rejectedBeforeExecution(provider.Rejected(invalidContinuationReference("native-a", http.StatusNotFound, "missing"))),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	second := activeProviderAttempt(t, fullHistory.nextState)
	routeFailover, err := reduce(context.Background(), fullHistory.nextState, providerCallFailed{
		attemptID: second.id,
		failure:   mayHaveExecuted(provider.Unavailable(canonical.NewBackendError("native-a", http.StatusServiceUnavailable, "unavailable", ""))),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if hasPreviousResponseDecision(routeFailover.nextState.effectiveChanges) {
		t.Fatalf("semantic operational failure manufactured continuation changes: %#v", routeFailover.nextState.effectiveChanges)
	}
	third := activeProviderAttempt(t, routeFailover.nextState)
	if len(routeFailover.nextState.providerCallAttempts) != 3 || third.id != 3 ||
		third.candidateIndex != 0 || !third.retry || third.requestChoice != providerRequestFullHistory {
		t.Fatalf("full-history operational retry = %#v", routeFailover.nextState.providerCallAttempts)
	}
}

func hasDecision(changes []compat.Change, feature canonical.CapabilityPath, outcome compat.Kind) bool {
	for _, decision := range changes {
		if decision.Capability == feature && decision.Kind == outcome {
			return true
		}
	}
	return false
}

func hasPreviousResponseDecision(changes []compat.Change) bool {
	for _, decision := range changes {
		if decision.Capability == canonical.RequestPreviousResponseResponses || decision.Capability == canonical.RequestPreviousResponse {
			return true
		}
	}
	return false
}

func completedResponseChanges(t *testing.T, outcome reducerOutcome) []compat.Change {
	t.Helper()
	terminal, ok := outcome.nextState.phase.(completedPhase)
	if !ok {
		t.Fatalf("phase = %T, want completedPhase", outcome.nextState.phase)
	}
	body := ClientTransportForTest(terminal.response).Body
	if body != nil {
		if _, err := io.ReadAll(body); err != nil {
			t.Fatalf("read completed response: %v", err)
		}
	}
	completion := responseCompletion(terminal.response)
	if completion == nil {
		t.Fatal("completed response has no completion truth")
	}
	snapshot := completion.Snapshot()
	if snapshot.State != wire.CompletionCompleted {
		t.Fatalf("completion state = %v, want completed (error %v)", snapshot.State, snapshot.Err)
	}
	return snapshot.Changes
}

func TestReducerOperationalRetryRetainsNativePreviousResponseOn500(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	prepared := mustNativeSession(t, s.input.request, target)
	s.prepared = &prepared
	started := beginPreparedProviderCall(t, s)
	active := activeProviderAttempt(t, started.nextState)
	failed, err := reduce(context.Background(), started.nextState, providerCallFailed{attemptID: active.id,
		failure: mayHaveExecuted(provider.Unavailable(canonical.NewBackendError("native-a", http.StatusInternalServerError, "failed", ""))),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	retry := activeProviderAttempt(t, failed.nextState)
	if retry.requestChoice != providerRequestPreferred || !retry.retry ||
		!nativePreviousResponseSent(retry.call.request) {
		t.Fatalf("500 operational retry = %#v", retry.providerCallAttempt)
	}
	if hasPreviousResponseDecision(failed.nextState.effectiveChanges) {
		t.Fatalf("500 emitted previous-response compatibility changes: %#v", failed.nextState.effectiveChanges)
	}
}

func TestTargetMismatchSelectsSemanticAndEmitsDropEvidence(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	otherTarget := requestpathTarget(t, "native-other")
	prepared := mustNativeSession(t, s.input.request, otherTarget)
	s.prepared = &prepared
	started := beginPreparedProviderCall(t, s)
	attempt := activeProviderAttempt(t, started.nextState)
	if nativePreviousResponseSent(attempt.call.request) {
		t.Fatal("target mismatch sent native previous response")
	}
	if hasPreviousResponseDecision(started.nextState.effectiveChanges) {
		t.Fatalf("target mismatch emitted effective changes before handoff: %#v", started.nextState.effectiveChanges)
	}
	ingress, err := attempt.call.backend.Transport.Send(context.Background(), attempt.call.document)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := reduce(context.Background(), started.nextState, providerIngressReceived{attemptID: attempt.id, ingress: ingress}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	changes := completedResponseChanges(t, completed)
	if !hasDecision(changes, canonical.RequestPreviousResponseResponses, compat.Approximation) {
		t.Fatalf("target mismatch handoff changes = %#v", changes)
	}
}

func TestNativeRequestDecodeFailureDoesNotTriggerSemanticRetry(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	prepared := mustNativeSession(t, s.input.request, target)
	s.prepared = &prepared
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
	if failedAttempt.status != providerCallAttemptFailed ||
		failedAttempt.failure.Attempt.Execution() != provider.ExecutionMayHaveOccurred {
		t.Fatalf("attempt = %#v, want possible provider execution", failedAttempt)
	}
	if hasPreviousResponseDecision(failed.nextState.effectiveChanges) {
		t.Fatalf("decode failure emitted previous-response compatibility changes: %#v", failed.nextState.effectiveChanges)
	}
}

func TestProviderResultAttemptIdentityRejectsUnknownAndNonActiveCallingAttempts(t *testing.T) {
	state := reducerTestState(t)
	if _, err := reduce(context.Background(), state, providerCallFailed{
		attemptID: 99,
		failure:   mayHaveExecuted(errors.New("unknown")),
	}, reducerRuntime()); err == nil {
		t.Fatal("unknown provider call attempt was accepted")
	}
	state.providerCallAttempts = []providerCallAttempt{
		{status: providerCallAttemptCalling},
		{status: providerCallAttemptCalling},
	}
	state.phase = callingProviderPhase{attemptID: 2}
	if _, err := reduce(context.Background(), state, providerCallFailed{
		attemptID: 1,
		failure:   mayHaveExecuted(errors.New("stale")),
	}, reducerRuntime()); err == nil {
		t.Fatal("non-active calling provider call attempt was accepted")
	}
}

func TestDuplicateProviderResultForTerminalAttemptIsInvariantError(t *testing.T) {
	started := startReducerProviderCall(t, reducerTestState(t))
	active := activeProviderAttempt(t, started.nextState)
	failed, err := reduce(context.Background(), started.nextState, providerCallFailed{
		attemptID: active.id,
		failure:   rejectedBeforeExecution(provider.Rejected(canonical.NewBackendError("a", http.StatusConflict, "rejected", ""))),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reduce(context.Background(), failed.nextState, providerCallFailed{
		attemptID: active.id,
		failure:   mayHaveExecuted(errors.New("duplicate")),
	}, reducerRuntime()); err == nil {
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
	if _, err := failProviderCallAttempt(
		acceptedState,
		1,
		mayHaveExecuted(errors.New("duplicate")),
	); err == nil {
		t.Fatal("terminal attempt accepted a second failure")
	}
	if _, err := failProviderCallAttempt(state, 1, provider.AttemptFailure{}); err == nil {
		t.Fatal("zero provider attempt failure was accepted")
	}
}

func TestProviderCallAttemptRejectsConsumedAttemptKey(t *testing.T) {
	s := reducerTestState(t)
	s.providerCallAttempts = []providerCallAttempt{{
		candidateIndex: 0,
		requestChoice:  providerRequestPreferred,
		providerRound:  0,
		retry:          false,
		status:         providerCallAttemptFailed,
	}}
	call := providerCall{
		backend:       provider.Backend{Target: provider.TargetSnapshot{TargetID: "target-a"}},
		providerRound: 0,
		replaySafety:  providerReplaySafe,
	}
	if _, err := beginProviderCallAttempt(
		s,
		providerCallSelection{candidateIndex: 0, requestChoice: providerRequestPreferred},
		call,
		nil,
	); err == nil {
		t.Fatal("consumed provider attempt key was reissued")
	}
}

func TestPreparationFailureDoesNotAllocateProviderCallAttempt(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{requestpathTarget(t, "unmarked-a")}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	runner := withRuntime(nil)
	runner.Runtime = candidateSelectiveRuntime{transport: bufferedProviderTransport(nil)}

	outcome, err := advanceProviderExecution(context.Background(), s, runner)
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

	outcome, err := advanceProviderExecution(context.Background(), s, runner)
	if err != nil {
		t.Fatal(err)
	}
	attempt := activeProviderAttempt(t, outcome.nextState)
	if len(outcome.nextState.providerCallAttempts) != 1 || attempt.id != 1 || attempt.candidateIndex != 1 {
		t.Fatalf("provider execution after local rejection = %#v", outcome.nextState)
	}
}

func requestWithResponsesReasoningContext(t *testing.T, request canonical.CanonicalRequest) canonical.CanonicalRequest {
	t.Helper()
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: request.Reasoning().ComputeField(), Disclosure: request.Reasoning().DisclosureField(),
		ResponsesContext: canonical.Specify(canonical.ResponsesReasoningContextAllTurns),
	})
	if err != nil {
		t.Fatal(err)
	}
	previous, hasPrevious := request.PreviousResponse()
	var previousPointer *canonical.ResponseRef
	if hasPrevious {
		previousPointer = &previous
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: request.ModelField(), Items: request.Items(),
		PreviousResponse: previousPointer, ToolPolicy: request.ToolPolicyField(),
		ToolCallBatch: request.ToolCallBatchField(), Controls: request.Controls(), Reasoning: reasoning,
		OutputFormat: request.OutputFormatField(),
	})
}

func closedToolImageRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	callID, _ := canonical.NewToolCallID("call_image")
	tool, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "inspect")
	input, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, _ := canonical.NewToolCallItem(callID, tool, canonical.NewJSONObjectToolInput(input))
	image, _ := canonical.NewInlineImage(canonical.ImageMediaPNG, []byte{0x89, 'P', 'N', 'G'}, canonical.Unspecified[canonical.ImageDetail]())
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{
		canonical.NewImageToolResultPart(image),
	}, false)
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{call, result},
	})
}

func requestpathTargetWithProtocol(t *testing.T, id string, rawProtocol string) routing.Target {
	t.Helper()
	targetID, _ := routing.ParseTargetID(id)
	model, _ := routing.ParseUpstreamModel("upstream-" + id)
	connection, _ := routing.NewCustomConnection("https://example.test/v1", nil)
	protocol, err := routing.ParseProtocol(rawProtocol, routing.ProviderCustom, func(routing.Provider, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestBackendResolutionMustPreserveResolvedTargetProjection(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{requestpathTarget(t, "target-a")}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	runner := withRuntime(nil)
	runner.Runtime = targetMutatingRuntime{candidateSelectiveRuntime{transport: bufferedProviderTransport(nil)}}

	outcome, err := advanceProviderExecution(context.Background(), s, runner)
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

func TestProviderCallRequirementsUseActualAttempt(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	prepared := mustNativeSession(t, s.input.request, target)
	s.prepared = &prepared
	started := beginPreparedProviderCall(t, s)
	attempt := activeProviderAttempt(t, started.nextState)
	if !attempt.nativePreviousResponse {
		t.Fatal("attempt ledger lost the native continuation realization")
	}
}

func TestUnrelatedUnsupportedFailureDoesNotBecomeNativeObservationOrRetry(t *testing.T) {
	s := reducerTestState(t)
	target := requestpathTarget(t, "native-a")
	s.route = routePlan{targets: []routing.Target{target}}
	prepared := mustNativeSession(t, s.input.request, target)
	s.prepared = &prepared
	started := beginPreparedProviderCall(t, s)
	active := activeProviderAttempt(t, started.nextState)
	failed, err := reduce(context.Background(), started.nextState, providerCallFailed{
		attemptID: active.id,
		failure:   rejectedBeforeExecution(provider.NewIncompatibleTarget("candidate cannot represent canonical tool choice")),
	}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := failed.nextState.phase.(callingProviderPhase); ok {
		t.Fatal("unrelated unsupported failure selected semantic history")
	}
	if hasPreviousResponseDecision(failed.nextState.effectiveChanges) {
		t.Fatalf("unrelated unsupported failure emitted native changes: %#v", failed.nextState.effectiveChanges)
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
			if candidatePreparationCanAdvance(err) {
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

	_, err = runExchange(context.Background(), runner, "ex_codec_fallback", "unknown", canonical.ClientFamilyResponses, delivery.BufferedDelivery(), testDecodedRequest(testCanonicalRequest("a")), nil, workspace, nil, canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	if providerCalls != 1 {
		t.Fatalf("provider calls = %d, want only compatible candidate B", providerCalls)
	}
}

func TestBedrockMantleStructuredOutputFallsBackBeforeTransport(t *testing.T) {
	targetID, _ := routing.ParseTargetID("mantle-a")
	model, _ := routing.ParseUpstreamModel("model-a")
	region, _ := routing.ParseBedrockRegion("us-east-1")
	connection, err := routing.NewBedrockConnection(region, "", "")
	if err != nil {
		t.Fatal(err)
	}
	messagesProtocol, err := routing.ParseProtocol(
		"messages",
		routing.ProviderBedrock,
		func(routing.Provider, string) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	mantle, err := routing.NewTarget(targetID, model, messagesProtocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	compatible := requestpathTargetWithProtocol(t, "responses-b", "responses")
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routeName, _ := routing.ParseRouteName("a")
	firstTier, _ := routing.NewTier([]routing.Target{mantle})
	secondTier, _ := routing.NewTier([]routing.Target{compatible})
	route, _ := routing.NewRoute(routeName, []routing.Tier{firstTier, secondTier})
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:   canonical.OutputFormatJSONSchema,
		Name:   "reply",
		Schema: canonical.NewRawJSONObject(`{"type":"object"}`),
		Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        canonical.Specify("a"),
		Items:        []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "hi")},
		OutputFormat: canonical.Specify(format),
	})
	var transported []string
	runner := withRuntime(nil)
	runner.Runtime = bedrockStructuredFallbackRuntime{transport: func(
		_ context.Context,
		target provider.TargetSnapshot,
		_ carrier.Document,
	) (provider.Ingress, error) {
		transported = append(transported, target.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(
			protocolkind.Responses,
			"application/json",
			nil,
			[]byte(`{"id":"resp_1","model":"m","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}]}`),
			carrier.Meta{},
		)}, nil
	}}
	_, err = runExchange(
		context.Background(),
		runner,
		"ex_mantle_structured_fallback",
		"unknown",
		canonical.ClientFamilyResponses,
		delivery.BufferedDelivery(),
		testDecodedRequest(request),
		nil,
		workspace,
		nil,
		canonical.NormalizedPathResponses,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(transported, []string{"responses-b"}) {
		t.Fatalf("transported targets = %v, want only compatible fallback", transported)
	}
}

func TestResponsesStopSequenceFallsBackToChatBeforeTransport(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routeName, _ := routing.ParseRouteName("a")
	responsesTarget := requestpathTargetWithProtocol(t, "responses-a", "responses")
	chatTarget := requestpathTargetWithProtocol(t, "chat-b", "chat_completions")
	firstTier, _ := routing.NewTier([]routing.Target{responsesTarget})
	secondTier, _ := routing.NewTier([]routing.Target{chatTarget})
	route, _ := routing.NewRoute(routeName, []routing.Tier{firstTier, secondTier})
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		StopSequences: []string{"END"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    canonical.Specify("a"),
		Items:    []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "hi")},
		Controls: controls,
	})
	var transported []string
	var providerDocument string
	runner := withRuntime(nil)
	runner.Runtime = protocolProjectionRuntime{transport: func(
		_ context.Context,
		target provider.TargetSnapshot,
		document carrier.Document,
	) (provider.Ingress, error) {
		transported = append(transported, target.TargetID)
		providerDocument = string(document.RawBytes())
		return provider.DocumentIngress{Document: carrier.NewDocument(
			protocolkind.ChatCompletions,
			"application/json",
			nil,
			[]byte(`{"id":"chat_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`),
			carrier.Meta{},
		)}, nil
	}}
	_, err = runExchange(
		context.Background(),
		runner,
		"ex_responses_stop_fallback",
		"unknown",
		canonical.ClientFamilyResponses,
		delivery.BufferedDelivery(),
		testDecodedRequest(request),
		nil,
		workspace,
		nil,
		canonical.NormalizedPathResponses,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(transported, []string{"chat-b"}) {
		t.Fatalf("transported targets = %v, want only Chat fallback", transported)
	}
	if !strings.Contains(providerDocument, `"stop":"END"`) {
		t.Fatalf("Chat fallback lost canonical stop sequence: %s", providerDocument)
	}
}

func TestFailedResponsesWebSearchLifecycleCompletesAsTypedProviderOutput(t *testing.T) {
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{
		requestpathTargetWithProtocol(t, "responses-a", "responses"),
		requestpathTargetWithProtocol(t, "responses-b", "responses"),
	}}
	prepared := mustBeginSession(t, s.input.request)
	s.prepared = &prepared
	runner := withRuntime(nil)
	runner.Runtime = protocolProjectionRuntime{transport: bufferedProviderTransport(nil)}

	started, err := advanceProviderExecution(context.Background(), s, runner)
	if err != nil {
		t.Fatal(err)
	}
	first := activeProviderAttempt(t, started.nextState)
	failedSearch := carrier.NewDocument(
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte(`{"id":"resp_1","model":"m","status":"completed","output":[{"type":"web_search_call","id":"ws_failed","status":"failed","action":{"type":"search","queries":["deadline"]}}]}`),
		carrier.Meta{},
	)
	completed, err := reduce(
		context.Background(),
		started.nextState,
		providerIngressReceived{attemptID: first.id, ingress: provider.DocumentIngress{Document: failedSearch}},
		runner,
	)
	if err != nil {
		t.Fatal(err)
	}
	recordedFirst, ok := findProviderCallAttempt(completed.nextState, first.id)
	if !ok || recordedFirst.failure != nil || recordedFirst.status != providerCallAttemptHandoffReady {
		t.Fatalf("completed Responses attempt = %#v", recordedFirst)
	}
	terminal, ok := completed.nextState.phase.(completedPhase)
	if !ok || terminal.target.TargetID != "responses-a" {
		t.Fatalf("terminal phase = %#v, want first target completion", completed.nextState.phase)
	}
	if len(completed.nextState.providerCallAttempts) != 1 {
		t.Fatalf("provider attempts = %#v, want no fallback", completed.nextState.providerCallAttempts)
	}
}

func TestUntypedPreparationFailureDoesNotMakeFallbackEligible(t *testing.T) {
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

	_, err = runExchange(context.Background(), runner, "ex_decision_is_not_policy", "unknown", canonical.ClientFamilyResponses, delivery.BufferedDelivery(), testDecodedRequest(testCanonicalRequest("a")), nil, workspace, nil, canonical.NormalizedPathResponses)
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
	}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	previous := session.Checkpoint{
		HistoryScheme: testHistoryScheme,
		History: func() *historyfingerprint.History {
			value := testExchangeHistoryForScheme(t, "responses/v1", "explicit-fallback")
			return &value
		}(),
		Request:  canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("a"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "turn one")}}),
		Response: previousResponse,
	}
	if _, err := store.StartSession(context.Background(), "dev", previous); err != nil {
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

	_, err = runExchange(context.Background(), runner, "ex_fallback", "unknown", canonical.ClientFamilyResponses, delivery.BufferedDelivery(), testDecodedRequest(request), nil, workspace, nil, canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	if store.getCalls != 1 {
		t.Fatalf("checkpoint Get calls = %d, want exactly 1 across fallback", store.getCalls)
	}
	if providerCalls != 3 {
		t.Fatalf("provider calls = %d, want one retry plus fallback", providerCalls)
	}
}
