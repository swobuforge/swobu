package exchange

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	evidence "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/wire"
)

func occurrenceFromDisposition(value provider.ExecutionDisposition) execution.Occurrence {
	switch value {
	case provider.DispositionNotDispatched, provider.DispositionRejectedBeforeExecution:
		return execution.DidNotOccur
	case provider.DispositionMayHaveOccurred:
		return execution.MayHaveOccurred
	default:
		return execution.MayHaveOccurred
	}
}

func runExchange(
	ctx context.Context,
	runner runtimeBundle,
	accessID accessidentity.ID,
	exchangeID string,
	canonicalExchangeID execution.ExchangeID,
	clientProduct clientfamily.Family,
	clientFamily canonical.ClientFamily,
	clientDelivery delivery.Delivery,
	decoded wire.ClientRequestResult,
	ingressChanges []compat.Change,
	workspace routing.Workspace,
	timing *observation.Timing,
	requestPath canonical.NormalizedPath,
	explicitExecutionAffinity executionaffinity.Key,
) (out RequestOutput, err error) {
	if clientDelivery.Mode == delivery.Streaming && runner.localToolDelivery == nil {
		workerCtx, cancel := context.WithCancel(ctx)
		bridge := newLocalToolDelivery(cancel)
		runner.localToolDelivery = bridge
		type result struct {
			out RequestOutput
			err error
		}
		completed := make(chan result, 1)
		go func() {
			out, err := runExchange(workerCtx, runner, accessID, exchangeID, canonicalExchangeID, clientProduct, clientFamily, clientDelivery, decoded, ingressChanges, workspace, timing, requestPath, explicitExecutionAffinity)
			if bridge.response != nil && err != nil {
				select {
				case bridge.events <- localToolDeliveryEvent{err: err}:
				case <-workerCtx.Done():
				}
			}
			close(bridge.events)
			completed <- result{out, err}
		}()
		select {
		case early := <-bridge.ready:
			early.ProductObservationInput = &ProductObservationInputData{}
			var once sync.Once
			early.FinalizeExecutionEvidence = func(result delivery.Result) {
				once.Do(func() {
					cancel()
					terminal := <-completed
					if terminal.out.ProductObservationInput != nil {
						*early.ProductObservationInput = *terminal.out.ProductObservationInput
					}
					if terminal.out.FinalizeExecutionEvidence != nil {
						terminal.out.FinalizeExecutionEvidence(result)
					}
				})
			}
			return early, nil
		case terminal := <-completed:
			if terminal.err != nil {
				cancel()
			} else {
				finalize := terminal.out.FinalizeExecutionEvidence
				terminal.out.FinalizeExecutionEvidence = func(result delivery.Result) {
					cancel()
					if finalize != nil {
						finalize(result)
					}
				}
			}
			return terminal.out, terminal.err
		case <-ctx.Done():
			cancel()
			terminal := <-completed
			return terminal.out, ctx.Err()
		}
	}
	var invocationOrdinal uint64
	var finalizeOnce sync.Once
	finalize := func(result delivery.Result) {
		finalizeOnce.Do(func() {
			settleExecutionEvidence(runner.ExecutionEvidence, canonicalExchangeID, result)
		})
	}
	// ExchangeStarted is appended by ingress before this function. Every
	// subsequent return therefore carries the same once-only settlement
	// capability, including validation, response-ID, and reducer failures.
	defer func() {
		if canonicalExchangeID.Valid() && out.FinalizeExecutionEvidence == nil {
			out.FinalizeExecutionEvidence = finalize
		}
	}()
	if err := validateCheckpointRuntime(runner); err != nil {
		return RequestOutput{}, err
	}
	if err := compat.ValidateChanges(ingressChanges); err != nil {
		return RequestOutput{}, canonical.InternalError(err.Error())
	}
	if !canonicalExchangeID.Valid() {
		return RequestOutput{}, canonical.InternalError("exchange identity is required")
	}
	responseID, err := allocateResponseID(ctx, exchangeID, runner.ResponseIDs)
	if err != nil {
		// No ExchangeSettled is emitted here: delivery owns finalization.
		return RequestOutput{}, err
	}
	if runner.localToolDelivery != nil {
		runner.localToolDelivery.projection = newLocalToolClientProjection(responseID)
	}
	var rebased *wire.RebasedRequest
	if decoded.RebasedRequest != nil {
		value := *decoded.RebasedRequest
		value.Request = value.Request.Clone()
		rebased = &value
	}
	s := exchangeState{
		input: exchangeInput{
			accessID: accessID, clientRequestID: exchangeID,
			canonicalExchangeID: canonicalExchangeID,
			clientProduct:       clientProduct, clientFamily: clientFamily, clientDelivery: clientDelivery,
			request:               canonical.CloneCanonicalRequest(decoded.Request),
			rebasedRequest:        rebased,
			requestFingerprint:    decoded.RequestFingerprint,
			mcpAccess:             decoded.MCPAccess,
			explicitCacheLocality: decoded.CacheLocality,
			workspace:             workspace, timing: timing,
			explicitExecutionAffinity: explicitExecutionAffinity,
			requestPath:               requestPath,
		},
		swobuResponseID:   responseID,
		phase:             startingPhase{},
		effectiveChanges:  compat.CloneChanges(ingressChanges),
		targetBackoff:     runner.TargetBackoff.snapshot(workspace.Slug().String()),
		invocationOrdinal: &invocationOrdinal,
	}
	defer func() {
		if s.mcp != nil {
			_ = s.mcp.Close()
		}
	}()
	var current exchangeEvent = exchangeStarted{}
	for steps := 0; steps < 1000; steps++ {
		tr, reduceErr := reduce(ctx, s, current, runner)
		if reduceErr != nil {
			return RequestOutput{FinalizeExecutionEvidence: finalize}, canonical.InternalError(reduceErr.Error())
		}
		s = tr.nextState
		switch p := s.phase.(type) {
		case completedPhase:
			out := terminalRequestOutput(s, p.response, p.target, summarizeRoutingEvidence(s.providerCallAttempts, s.evaluatedCandidateCount, true))
			out.FinalizeExecutionEvidence = finalize
			return out, nil
		case failedPhase:
			out := terminalRequestOutput(s, nil, p.target, summarizeRoutingEvidence(s.providerCallAttempts, s.evaluatedCandidateCount, false))
			out.FinalizeExecutionEvidence = finalize
			return out, p.problem
		}
		if tr.command == nil {
			return RequestOutput{}, canonical.InternalError(fmt.Sprintf("exchange invariant: active phase %T produced no command", s.phase))
		}
		call, providerCall := tr.command.(callProviderCommand)
		if mcpCall, ok := tr.command.(callMCPCommand); ok {
			if phase, ok := s.phase.(callingMCPPhase); ok && phase.invocationID.Valid() {
				mcpCall.invocationID = phase.invocationID
				mcpCall.evidence = runner.ExecutionEvidence
				tr.command = mcpCall
			}
		}
		var observation targetObservation
		if providerCall {
			observation = runner.TargetBackoff.begin(workspace.Slug().String(), call.backend.Target)
			logProviderAttemptStarted(s, call)
			tr.command = call
		}
		started := time.Now()
		current = executeCommand(ctx, tr.command)
		if providerCall {
			if result, ok := current.(providerCallFailed); !ok || result.failure.Execution() != provider.DispositionNotDispatched {
				_, _ = startModelInvocation(&s, call, runner.ExecutionEvidence, started)
			}
			emitProviderInvocationTransportSettlement(runner.ExecutionEvidence, s, call, current)
			logProviderAttemptCommandResult(s, call, current, time.Since(started))
			runner.TargetBackoff.observe(observation, current)
			settleAccountingAttempt(runner, s, call, current)
		}
	}
	err = canonical.InternalError("exchange transition limit reached")
	// No ExchangeSettled is emitted here: delivery owns finalization.
	return RequestOutput{}, err
}

func settleAccountingAttempt(runner runtimeBundle, state exchangeState, call callProviderCommand, event exchangeEvent) {
	if runner.Accounting == nil {
		return
	}
	attempt, ok := findProviderCallAttempt(state, call.attemptID)
	if !ok || attempt.accountingOperation == "" {
		return
	}
	if _, received := event.(providerIngressReceived); received {
		return
	}
	uncertain := false
	if failed, ok := event.(providerCallFailed); ok {
		uncertain = failed.failure.Execution() == provider.DispositionMayHaveOccurred
	}
	if attempt.accountingWindow != "" {
		_ = runner.Accounting.SettleReservation(state.input.workspace.Slug().String(), attempt.accountingWindow, attempt.accountingOperation, attempt.accountingAmountCents, uncertain)
	} else {
		_ = runner.Accounting.Settle(state.input.workspace.Slug().String(), attempt.accountingOperation, attempt.accountingAmountCents, uncertain)
	}
}

func startModelInvocation(state *exchangeState, call callProviderCommand, sink observation.ExecutionEvidenceSink, at time.Time) (execution.InvocationID, error) {
	if state == nil || !state.inputExchangeID().Valid() {
		return execution.InvocationID{}, errors.New("model invocation requires exchange identity")
	}
	if state.invocationOrdinal == nil {
		return execution.InvocationID{}, errors.New("invocation ordinal allocator is missing")
	}
	resource, err := modelInvocationResource(call)
	if err != nil {
		return execution.InvocationID{}, err
	}
	index, ok := providerCallAttemptIndex(*state, call.attemptID)
	if !ok {
		return execution.InvocationID{}, errors.New("model invocation requires an existing provider attempt")
	}
	id, err := execution.NewInvocationID(state.inputExchangeID(), *state.invocationOrdinal+1)
	if err != nil {
		return execution.InvocationID{}, err
	}
	started, err := evidence.NewModelInvocationStarted(id, at, resource)
	if err != nil {
		return execution.InvocationID{}, err
	}
	*state.invocationOrdinal++
	attempts := append([]providerCallAttempt(nil), state.providerCallAttempts...)
	attempts[index].executionInvocationID = id
	state.providerCallAttempts = attempts
	appendExecutionEvidence(sink, started, id.ExchangeID().String())
	return id, nil
}

func modelInvocationResource(call callProviderCommand) (evidence.ModelResource, error) {
	provider, err := routing.ParseProvider(call.backend.Target.ProviderSpec, func(string) bool { return true })
	if err != nil {
		return evidence.ModelResource{}, err
	}
	model, err := routing.ParseUpstreamModel(call.backend.Target.Model)
	if err != nil {
		return evidence.ModelResource{}, err
	}
	target, err := routing.ParseTargetID(call.backend.Target.TargetID)
	if err != nil {
		return evidence.ModelResource{}, err
	}
	resource, err := evidence.NewModelResource(call.route, target, routing.TargetVersion(call.backend.Target.TargetVersion), provider, call.backend.Target.ProtocolKind, model, evidence.ExchangeAttempt)
	if err != nil {
		return evidence.ModelResource{}, err
	}
	if id, revision, bound := call.backend.Target.ConnectionIdentity(); bound {
		return resource.BindConnection(id, revision)
	}
	return resource, nil
}

func settleExecutionEvidence(sink observation.ExecutionEvidenceSink, id execution.ExchangeID, result delivery.Result) {
	if sink == nil {
		return
	}
	code := canonical.TerminalErrorCode(result.Err)
	settled, err := evidence.NewExchangeSettled(id, time.Now(), result.Kind, result.HTTPStatus, code)
	if err != nil {
		logExecutionEvidenceFailure(id.String(), err)
		return
	}
	appendExecutionEvidence(sink, settled, id.String())
}

func logExecutionEvidenceFailure(exchangeID string, err error) {
	slog.Warn("execution evidence append failed", "component", "exchange", "event", "execution_evidence_append_failed", "exchange_id", exchangeID, "error", err)
}

func appendExecutionEvidence(sink observation.ExecutionEvidenceSink, event evidence.Event, exchangeID string) {
	if sink != nil {
		if err := sink.AppendExecutionEvidence(event); err != nil {
			logExecutionEvidenceFailure(exchangeID, err)
		}
	}
}

func logProviderAttemptStarted(state exchangeState, call callProviderCommand) {
	attempt, _ := findProviderCallAttempt(state, call.attemptID)
	slog.Debug("provider attempt started",
		"component", "exchange", "event", "provider_attempt_started",
		"request_id", state.correlationID(), "attempt", int(call.attemptID),
		"target_id", call.backend.Target.TargetID, "provider", call.backend.Target.ProviderSpec,
		"model", call.backend.Target.Model, "provider_round", attempt.providerRound,
		"candidate_index", attempt.candidateIndex, "native_previous_response", attempt.nativePreviousResponse,
	)
}

// logProviderAttemptCommandResult records only the provider command boundary.
// Successful ingress still requires reducer-owned validation, decoding, and
// client-response construction before the attempt is accepted for handoff.
func logProviderAttemptCommandResult(state exchangeState, call callProviderCommand, event exchangeEvent, duration time.Duration) {
	attrs := []any{"component", "exchange",
		"request_id", state.correlationID(), "attempt", int(call.attemptID),
		"target_id", call.backend.Target.TargetID, "provider", call.backend.Target.ProviderSpec,
		"model", call.backend.Target.Model, "duration", duration}
	if failed, ok := event.(providerCallFailed); ok {
		attrs = append(attrs, "event", "provider_attempt_finished")
		failureClass, level := providerFailureLogClassification(failed.failure.Cause())
		attrs = append(attrs, "outcome", "failed_before_handoff", "execution", executionValue(failed.failure.Execution()),
			"failure_class", failureClass, "failure_stage", "provider_transport")
		var backendErr canonical.BackendError
		if errors.As(failed.failure.Cause(), &backendErr) {
			attrs = appendBackendErrorMetadata(attrs, backendErr)
			if attempt, ok := findProviderCallAttempt(state, call.attemptID); ok {
				logStructuredProviderErrorDetail(state, call.attemptID, attempt, backendErr)
			}
		}
		if level == slog.LevelDebug {
			slog.LogAttrs(context.Background(), slog.LevelDebug, "provider attempt canceled", anyAttrs(attrs)...)
			return
		}
		slog.LogAttrs(context.Background(), level, "provider attempt failed", anyAttrs(attrs)...)
		return
	}
	attrs = append(attrs, "event", "provider_attempt_ingress_received")
	slog.LogAttrs(context.Background(), slog.LevelDebug, "provider attempt ingress received", anyAttrs(attrs)...)
}

func emitProviderInvocationTransportSettlement(sink observation.ExecutionEvidenceSink, state exchangeState, call callProviderCommand, event exchangeEvent) {
	if sink == nil {
		return
	}
	attempt, ok := findProviderCallAttempt(state, call.attemptID)
	if !ok || !attempt.executionInvocationID.Valid() {
		return
	}
	failed, ok := event.(providerCallFailed)
	if !ok {
		return
	}
	possibility := provider.DispositionMayHaveOccurred
	switch failed.failure.Execution() {
	case provider.DispositionNotDispatched:
		possibility = provider.DispositionNotDispatched
	case provider.DispositionRejectedBeforeExecution:
		possibility = provider.DispositionRejectedBeforeExecution
	}
	class := modelFailureFromCause(failed.failure.Cause())
	outcome := execution.Failed
	if errors.Is(failed.failure.Cause(), context.Canceled) {
		outcome, class = execution.Cancelled, ""
	}
	settled, err := evidence.NewModelInvocationSettled(attempt.executionInvocationID, time.Now(), outcome, occurrenceFromDisposition(possibility), class, canonical.NewUnknownTokenUsage())
	if err == nil {
		appendExecutionEvidence(sink, settled, state.correlationID())
	}
}

// modelFailureFromCause maps provider-owned typed causes into canonical model
// evidence. Logging severity is deliberately not consulted: logs project this
// semantic fact, never define it.
func modelFailureFromCause(err error) evidence.ModelFailure {
	err = provider.NormalizeFailure(err)
	var timeout provider.TimeoutError
	if errors.As(err, &timeout) {
		return evidence.FailureTimeout
	}
	var rejected provider.RejectedError
	if errors.As(err, &rejected) {
		return evidence.FailureRejected
	}
	var unavailable provider.UnavailableError
	if errors.As(err, &unavailable) {
		return evidence.FailureUnavailable
	}
	var targetUnavailable provider.TargetUnavailableError
	if errors.As(err, &targetUnavailable) {
		return evidence.FailureTargetUnavailable
	}
	var targetIncompatible provider.TargetIncompatibleError
	if errors.As(err, &targetIncompatible) {
		return evidence.FailureTargetIncompatible
	}
	var invalid provider.InvalidRequestError
	if errors.As(err, &invalid) {
		return evidence.FailureInvalidRequest
	}
	return evidence.FailureInternal
}

func emitProviderInvocationSettlement(sink observation.ExecutionEvidenceSink, state exchangeState, attempt providerCallAttempt, outcome execution.Outcome, occurrence execution.Occurrence, class evidence.ModelFailure, usage canonical.TokenUsage) {
	if sink == nil || !attempt.executionInvocationID.Valid() {
		return
	}
	settled, err := evidence.NewModelInvocationSettled(attempt.executionInvocationID, time.Now(), outcome, occurrence, class, usage)
	if err == nil {
		appendExecutionEvidence(sink, settled, state.correlationID())
	}
}

func logProviderAttemptFailedBeforeHandoff(state exchangeState, attemptID providerCallAttemptID, attempt providerCallAttempt, err error) {
	stage := "unknown"
	if exact, ok := wire.ResponseFailureStage(err); ok {
		stage = exact
	}
	failureClass, level := providerFailureLogClassification(err)
	canceled := errors.Is(err, context.Canceled)
	if canceled {
		failureClass, level = "canceled", slog.LevelDebug
	}
	attrs := []any{
		"component", "exchange", "event", "provider_attempt_finished",
		"request_id", state.correlationID(), "attempt", int(attemptID),
		"target_id", attempt.target.TargetID, "provider", attempt.target.ProviderSpec,
		"model", attempt.target.Model, "outcome", "failed_before_handoff",
		"failure_class", failureClass, "failure_stage", stage, "error_type", safeErrorType(err),
	}
	if canceled {
		attrs = append(attrs, "error_origin", "client")
	} else {
		var backendErr canonical.BackendError
		if errors.As(err, &backendErr) {
			attrs = appendBackendErrorMetadata(attrs, backendErr)
			logStructuredProviderErrorDetail(state, attemptID, attempt, backendErr)
		}
		logPrivateDiagnosticCause(state, attemptID, attempt, err)
	}
	if level == slog.LevelDebug {
		slog.LogAttrs(context.Background(), level, "provider attempt canceled before handoff", anyAttrs(attrs)...)
		return
	}
	slog.LogAttrs(context.Background(), level, "provider attempt failed before handoff", anyAttrs(attrs)...)
}

func logPrivateDiagnosticCause(state exchangeState, attemptID providerCallAttemptID, attempt providerCallAttempt, err error) {
	var canonicalErr canonical.Error
	if !errors.As(err, &canonicalErr) || canonicalErr.DiagnosticCause == nil {
		return
	}
	slog.Debug("provider decoder diagnostic",
		"component", "exchange", "event", "provider_decoder_diagnostic",
		"request_id", state.correlationID(), "attempt", int(attemptID), "target_id", attempt.target.TargetID,
		"error_type", fmt.Sprintf("%T", canonicalErr.DiagnosticCause),
	)
}

func appendTypedTerminalError(attrs []any, err error) ([]any, slog.Level) {
	if errors.Is(err, context.Canceled) {
		return append(attrs, "error_origin", "client"), slog.LevelDebug
	}
	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		return appendBackendErrorMetadata(attrs, backendErr), slog.LevelWarn
	}
	var canonicalErr canonical.Error
	if errors.As(err, &canonicalErr) {
		return append(attrs,
			"error_origin", string(canonicalErr.Origin),
			"error_code", string(canonicalErr.Code),
			"error_message", canonicalErr.Message,
		), slog.LevelError
	}
	return attrs, slog.LevelError
}

func appendBackendErrorMetadata(attrs []any, backendErr canonical.BackendError) []any {
	attrs = append(attrs, "error_origin", string(canonical.ErrorOriginBackend), "status_code", backendErr.StatusCode)
	if backendErr.SourceProtocol != "" {
		attrs = append(attrs, "source_protocol", backendErr.SourceProtocol.String())
	}
	if backendErr.ProviderError == nil {
		return attrs
	}
	detail := backendErr.ProviderError
	if detail.Type != "" {
		attrs = append(attrs, "backend_error_type", detail.Type)
	}
	if detail.Code != "" {
		attrs = append(attrs, "backend_error_code", detail.Code)
	}
	if detail.RequestID != "" {
		attrs = append(attrs, "backend_request_id", detail.RequestID)
	}
	return attrs
}

func logStructuredProviderErrorDetail(state exchangeState, attemptID providerCallAttemptID, attempt providerCallAttempt, backendErr canonical.BackendError) {
	if backendErr.ProviderError == nil {
		return
	}
	detail := backendErr.ProviderError
	attrs := []any{
		"component", "exchange", "event", "provider_error_detail",
		"request_id", state.correlationID(), "attempt", int(attemptID), "target_id", attempt.target.TargetID,
		"backend_error_type", detail.Type, "backend_error_code", detail.Code,
		"backend_error_param", detail.Param,
		"backend_request_id", detail.RequestID, "source_protocol", backendErr.SourceProtocol.String(),
	}
	slog.LogAttrs(context.Background(), slog.LevelDebug, "structured provider error detail", anyAttrs(attrs)...)
}

func safeErrorType(err error) string {
	err = wire.ResponseFailureCause(err)
	if errors.Is(err, context.Canceled) {
		return "context.Canceled"
	}
	if err == nil {
		return "<nil>"
	}
	return reflect.TypeOf(err).String()
}

// providerFailureLogClassification maps provider-normalized typed failure
// truth to the logging severity contract: cancellation is debug, recoverable
// target failure is warning, and internal failure is error. Message prose,
// provider identity, and eventual route recovery never select severity.
func providerFailureLogClassification(err error) (string, slog.Level) {
	err = provider.NormalizeFailure(err)
	var unavailable provider.UnavailableError
	if errors.As(err, &unavailable) {
		return "unavailable", slog.LevelWarn
	}
	var rejected provider.RejectedError
	if errors.As(err, &rejected) {
		return "rejected", slog.LevelWarn
	}
	var invalid provider.InvalidRequestError
	if errors.As(err, &invalid) {
		return "invalid_request", slog.LevelWarn
	}
	var canceled provider.CancelledError
	if errors.As(err, &canceled) {
		return "canceled", slog.LevelDebug
	}
	return "internal", slog.LevelError
}

func executionValue(execution provider.ExecutionDisposition) string {
	switch execution {
	case provider.DispositionNotDispatched, provider.DispositionRejectedBeforeExecution, provider.DispositionMayHaveOccurred:
		return string(execution)
	default:
		return "unknown"
	}
}

func anyAttrs(values []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(values)/2)
	for i := 0; i+1 < len(values); i += 2 {
		attrs = append(attrs, slog.Any(values[i].(string), values[i+1]))
	}
	return attrs
}

func executeCommand(ctx context.Context, cmd command) exchangeEvent {
	switch c := cmd.(type) {
	case loadCheckpointCommand:
		var record continuity.Checkpoint
		var found bool
		var err error
		if c.explicit {
			record, found, err = c.store.Get(ctx, c.workspaceSlug, c.reference)
		} else {
			record, found, err = c.store.FindByHistory(ctx, c.workspaceSlug, c.history)
		}
		return checkpointLoaded{record: record, found: found, err: err}
	case prepareMCPCommand:
		full, run, changes, err := mcp.Open(ctx, c.full, c.access)
		return mcpPrepared{full: full, run: run, changes: changes, err: err}
	case callProviderCommand:
		ingress, err := c.backend.Transport.Send(ctx, c.document)
		if err != nil {
			failure, ok := provider.AsAttemptFailure(err)
			if !ok {
				panic(fmt.Sprintf("exchange invariant: bound provider transport returned %T without attempt facts", err))
			}
			return providerCallFailed{attemptID: c.attemptID, failure: failure}
		}
		return providerIngressReceived{attemptID: c.attemptID, ingress: ingress}
	case refreshCredentialCommand:
		err := ctx.Err()
		var material string
		if err == nil {
			material, err = c.refresh(ctx)
		}
		if err == nil {
			err = c.publish(ctx, c.target, material)
		}
		return credentialRefreshed{attemptID: c.attemptID, err: err}
	case characterizeTargetFactsCommand:
		resolutions := make(map[provider.TargetFact]bool, len(c.facts))
		for _, fact := range c.facts {
			transport := &characterizationTransport{nextInvocation: c.nextInvocation, sink: c.sink, target: c.target, route: c.route}
			backend := c.backend
			backend.Transport = transport
			transport.delegate = c.backend.Transport
			resolution := backend.CharacterizeTargetFact(ctx, fact)
			if resolution.Conclusive {
				resolutions[fact] = resolution.Value
			}
		}
		return targetFactsCharacterized{attemptID: c.attemptID, generation: c.generation, resolutions: resolutions}
	case callMCPCommand:
		if c.invocationID.Valid() {
			emitMCPInvocationStarted(c.evidence, c.resource, c.invocationID, c.invocationID.ExchangeID().String())
		}
		result, err := c.prepared.Execute(ctx)
		return mcpToolReturned{result: result, err: err}
	case beginMCPBatchCommand:
		return mcpBatchStarted{err: c.run.BeginBatch(c.calls)}
	default:
		panic(fmt.Sprintf("exchange invariant: unsupported closed command %T", cmd))
	}
}

type characterizationTransport struct {
	delegate       provider.Transport
	nextInvocation func() execution.InvocationID
	sink           observation.ExecutionEvidenceSink
	target         provider.TargetSnapshot
	route          routing.RouteName
	pending        []execution.InvocationID
}

func (t *characterizationTransport) Send(ctx context.Context, document carrier.Document) (provider.Ingress, error) {
	if t.nextInvocation == nil || t.sink == nil {
		return t.delegate.Send(ctx, document)
	}
	startedAt := time.Now()
	ingress, sendErr := t.delegate.Send(ctx, document)
	if typed, ok := provider.AsAttemptFailure(sendErr); ok && typed.Execution() == provider.DispositionNotDispatched {
		return ingress, sendErr
	}
	providerID, err := routing.ParseProvider(t.target.ProviderSpec, func(string) bool { return true })
	if err != nil {
		return ingress, sendErr
	}
	model, err := routing.ParseUpstreamModel(t.target.Model)
	if err != nil {
		return ingress, sendErr
	}
	targetID, err := routing.ParseTargetID(t.target.TargetID)
	if err != nil {
		return ingress, sendErr
	}
	resource, err := evidence.NewModelResource(t.route, targetID, routing.TargetVersion(t.target.TargetVersion), providerID, t.target.ProtocolKind, model, evidence.TargetCharacterization)
	if err == nil {
		if id, revision, bound := t.target.ConnectionIdentity(); bound {
			resource, err = resource.BindConnection(id, revision)
		}
	}
	if err != nil {
		return ingress, sendErr
	}
	id := t.nextInvocation()
	if !id.Valid() {
		return ingress, sendErr
	}
	started, err := evidence.NewModelInvocationStarted(id, startedAt, resource)
	if err == nil && t.sink != nil {
		appendExecutionEvidence(t.sink, started, id.ExchangeID().String())
	}
	if sendErr != nil {
		possibility := provider.DispositionMayHaveOccurred
		failure := modelFailureFromCause(sendErr)
		outcome := execution.Failed
		if typed, ok := provider.AsAttemptFailure(sendErr); ok {
			possibility = typed.Execution()
			failure = modelFailureFromCause(typed.Cause())
		}
		if errors.Is(sendErr, context.Canceled) {
			outcome, failure = execution.Cancelled, ""
		}
		settled, settleErr := evidence.NewModelInvocationSettled(id, time.Now(), outcome, occurrenceFromDisposition(possibility), failure, canonical.NewUnknownTokenUsage())
		if settleErr == nil && t.sink != nil {
			appendExecutionEvidence(t.sink, settled, id.ExchangeID().String())
		}
		return ingress, sendErr
	}
	// Transport success is only external ingress. Defer settlement until the
	// codec characterizer has consumed the fixture result, so a decode/stream
	// failure cannot be misreported as a successful execution at ingress.
	t.pending = append(t.pending, id)
	return ingress, sendErr
}

// FixtureSettled consumes exactly one successfully dispatched fixture. The
// aggregate fact resolution must never rewrite this invocation's terminal.
func (t *characterizationTransport) FixtureSettled(terminalErr error) {
	if t == nil || len(t.pending) == 0 {
		return
	}
	id := t.pending[0]
	t.pending = t.pending[1:]
	outcome, occurrence, failure := execution.Succeeded, execution.Occurred, evidence.ModelFailure("")
	if terminalErr != nil {
		outcome, occurrence, failure = execution.Failed, execution.MayHaveOccurred, evidence.FailureResponseInvalid
		if errors.Is(terminalErr, context.Canceled) {
			outcome, failure = execution.Cancelled, ""
		}
	}
	settled, err := evidence.NewModelInvocationSettled(id, time.Now(), outcome, occurrence, failure, canonical.NewUnknownTokenUsage())
	if err == nil && t.sink != nil {
		appendExecutionEvidence(t.sink, settled, id.ExchangeID().String())
	}
}

// terminalRoutingEvidence is the bounded routing-recovery summary carried from
// the exchange state machine to the terminal traffic event. It holds no content
// or identity. The candidate count is consumed here only to derive whether a
// fallback recovered the request; it is not retained (the terminal event needs
// the recovery fact, not the candidate tally).
type terminalRoutingEvidence struct {
	providerCallCount int
	fallbackRecovered bool
}

// summarizeRoutingEvidence derives the recovery fact from the contiguous route
// candidates evaluated by deterministic selection and codec lowering: a request
// completed after more than one candidate was a fallback recovery.
func summarizeRoutingEvidence(attempts []providerCallAttempt, candidateCount int, completed bool) terminalRoutingEvidence {
	summary := terminalRoutingEvidence{
		providerCallCount: len(attempts),
	}
	summary.fallbackRecovered = completed && candidateCount > 1
	return summary
}

func terminalRequestOutput(state exchangeState, response ClientResponse, target provider.TargetSnapshot, routing terminalRoutingEvidence) RequestOutput {
	var evidence *ProductObservationInputData
	if target.TargetID != "" {
		evidence = &ProductObservationInputData{
			clientProduct: state.input.clientProduct, clientProtocol: state.input.clientFamily,
			requestedModel: telemetryRequestedModel(state.input.request.Model(), state.route.name.String()),
			resolvedModel:  telemetryResolvedModel(state.route.name.String(), target.Model),
			requestPath:    state.input.requestPath, provider: string(target.ProviderSpec),
			targetProtocol: target.ProtocolKind.String(), responseStatus: clientResponseStatus(response),
			crossProtocol: string(state.input.clientFamily) != target.ProtocolKind.String(),
			attemptCount:  routing.providerCallCount, fallbackRecovered: routing.fallbackRecovered,
			continuityRecovered: state.resolvedFrom != "", wireMutated: len(state.effectiveChanges) > 0,
		}
	}
	return RequestOutput{Response: response, Target: target, ProductObservationInput: evidence, Compatibility: responseCompletion(response), AttemptCount: routing.providerCallCount}
}

// Model categories are classified before the telemetry seam; raw model names
// never become part of the terminal observation or remote report.
func telemetryRequestedModel(requested, route string) string {
	switch {
	case requested == "":
		return "unknown"
	case requested == "default":
		return "default"
	case route != "" && requested == route:
		return "configured"
	default:
		return "custom"
	}
}

func telemetryResolvedModel(route, providerModel string) string {
	if route != "" && providerModel != "" {
		return "configured"
	}
	return "unknown"
}
