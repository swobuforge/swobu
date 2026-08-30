package exchange

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/wire"
)

func runExchange(
	ctx context.Context,
	runner runtimeBundle,
	exchangeID string,
	clientHandler trafficevidence.ClientHandler,
	clientFamily canonical.ClientFamily,
	clientDelivery delivery.Delivery,
	decoded wire.ClientRequestResult,
	ingressChanges []compat.Change,
	workspace routing.Workspace,
	timing *trafficevidence.Timing,
	requestPath canonical.NormalizedPath,
) (RequestOutput, error) {
	if err := validateCheckpointRuntime(runner); err != nil {
		return RequestOutput{}, err
	}
	if err := compat.ValidateChanges(ingressChanges); err != nil {
		return RequestOutput{}, canonical.InternalError(err.Error())
	}
	responseID, err := allocateResponseID(ctx, exchangeID, runner.ResponseIDs)
	if err != nil {
		return RequestOutput{}, err
	}
	var rebased *wire.RebasedRequest
	if decoded.RebasedRequest != nil {
		value := *decoded.RebasedRequest
		value.Request = value.Request.Clone()
		rebased = &value
	}
	s := exchangeState{
		input: exchangeInput{
			exchangeID: exchangeID, clientHandler: clientHandler,
			clientFamily: clientFamily, clientDelivery: clientDelivery,
			request:               canonical.CloneCanonicalRequest(decoded.Request),
			rebasedRequest:        rebased,
			requestFingerprint:    decoded.RequestFingerprint,
			mcpAccess:             decoded.MCPAccess,
			explicitCacheLocality: decoded.CacheLocality,
			workspace:             workspace, timing: timing,
			requestPath: requestPath,
		},
		swobuResponseID:  responseID,
		phase:            startingPhase{},
		effectiveChanges: compat.CloneChanges(ingressChanges),
		targetBackoff:    runner.TargetBackoff.snapshot(workspace.Slug().String()),
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
			return RequestOutput{}, canonical.InternalError(reduceErr.Error())
		}
		s = tr.nextState
		switch p := s.phase.(type) {
		case completedPhase:
			return terminalRequestOutput(s.input, p.response, p.target, s.route.name, summarizeRoutingEvidence(s.providerCallAttempts, s.evaluatedCandidateCount, true), terminalReusablePrefix(s)), nil
		case failedPhase:
			return terminalRequestOutput(s.input, nil, p.target, s.route.name, summarizeRoutingEvidence(s.providerCallAttempts, s.evaluatedCandidateCount, false), terminalReusablePrefix(s)), p.problem
		}
		if tr.command == nil {
			return RequestOutput{}, canonical.InternalError(fmt.Sprintf("exchange invariant: active phase %T produced no command", s.phase))
		}
		call, providerCall := tr.command.(callProviderCommand)
		if providerCall {
			appendProviderInflightEvidence(ctx, runner.TrafficEvidence, s)
		}
		var observation targetObservation
		if providerCall {
			observation = runner.TargetBackoff.begin(workspace.Slug().String(), call.backend.Target)
			logProviderAttemptStarted(s, call)
		}
		started := time.Now()
		current = executeCommand(ctx, tr.command)
		if providerCall {
			logProviderAttemptCommandResult(s, call, current, time.Since(started))
			runner.TargetBackoff.observe(observation, current)
		}
	}
	return RequestOutput{}, canonical.InternalError("exchange transition limit reached")
}

func logProviderAttemptStarted(state exchangeState, call callProviderCommand) {
	attempt, _ := findProviderCallAttempt(state, call.attemptID)
	slog.Debug("provider attempt started",
		"component", "exchange", "event", "provider_attempt_started",
		"request_id", state.input.exchangeID, "attempt", int(call.attemptID),
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
		"request_id", state.input.exchangeID, "attempt", int(call.attemptID),
		"target_id", call.backend.Target.TargetID, "provider", call.backend.Target.ProviderSpec,
		"model", call.backend.Target.Model, "duration", duration}
	if failed, ok := event.(providerCallFailed); ok {
		attrs = append(attrs, "event", "provider_attempt_finished")
		failureClass, level := providerFailureLogClassification(failed.failure.Cause())
		attrs = append(attrs, "outcome", "failed_before_handoff", "execution", executionValue(failed.failure.Execution()),
			"failure_class", failureClass, "failure_stage", "provider_transport")
		var backendErr canonical.BackendError
		if errors.As(failed.failure.Cause(), &backendErr) {
			attrs = append(attrs, "status_code", backendErr.StatusCode)
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

func observeProviderAttemptTerminal(state exchangeState, attemptID providerCallAttemptID, attempt providerCallAttempt, completion *wire.ResponseCompletion) {
	if completion == nil {
		return
	}
	completion.OnTerminal(func(snapshot wire.ResponseCompletionSnapshot) {
		attrs := []any{
			"component", "exchange", "event", "provider_attempt_finished",
			"request_id", state.input.exchangeID, "attempt", int(attemptID),
			"target_id", attempt.target.TargetID, "provider", attempt.target.ProviderSpec,
			"model", attempt.target.Model,
		}
		if snapshot.State == wire.CompletionCompleted {
			attrs = append(attrs, "outcome", "completed")
			slog.LogAttrs(context.Background(), slog.LevelDebug, "provider attempt finished", anyAttrs(attrs)...)
			return
		}
		stage := "unknown"
		if exact, ok := wire.ResponseFailureStage(snapshot.Err); ok {
			stage = exact
		}
		attrs = append(attrs,
			"outcome", "aborted_after_handoff",
			"failure_stage", stage,
			"error_type", safeErrorType(snapshot.Err),
		)
		attrs, level := appendTypedTerminalError(attrs, snapshot.Err)
		slog.LogAttrs(context.Background(), level, "provider attempt aborted after handoff", anyAttrs(attrs)...)
	})
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
		"request_id", state.input.exchangeID, "attempt", int(attemptID),
		"target_id", attempt.target.TargetID, "provider", attempt.target.ProviderSpec,
		"model", attempt.target.Model, "outcome", "failed_before_handoff",
		"failure_class", failureClass, "failure_stage", stage, "error_type", safeErrorType(err),
	}
	if canceled {
		attrs = append(attrs, "error_origin", "client")
	} else {
		var backendErr canonical.BackendError
		if errors.As(err, &backendErr) {
			attrs = append(attrs,
				"error_origin", string(canonical.ErrorOriginBackend),
				"status_code", backendErr.StatusCode,
			)
		}
	}
	if level == slog.LevelDebug {
		slog.LogAttrs(context.Background(), level, "provider attempt canceled before handoff", anyAttrs(attrs)...)
		return
	}
	slog.LogAttrs(context.Background(), level, "provider attempt failed before handoff", anyAttrs(attrs)...)
}

func appendTypedTerminalError(attrs []any, err error) ([]any, slog.Level) {
	if errors.Is(err, context.Canceled) {
		return append(attrs, "error_origin", "client"), slog.LevelDebug
	}
	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		return append(attrs,
			"error_origin", string(canonical.ErrorOriginBackend),
			"status_code", backendErr.StatusCode,
		), slog.LevelWarn
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

func executionValue(execution provider.ExecutionPossibility) string {
	switch execution {
	case provider.ExecutionNotDispatched:
		return "not_dispatched"
	case provider.ExecutionRejectedBeforeExecution:
		return "rejected_before_execution"
	case provider.ExecutionMayHaveOccurred:
		return "may_have_occurred"
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

func appendProviderInflightEvidence(ctx context.Context, sink observation.TrafficEventSink, state exchangeState) {
	if sink == nil || len(state.providerCallAttempts) == 0 {
		return
	}
	attempt := state.providerCallAttempts[len(state.providerCallAttempts)-1]
	requestID, err := trafficevidence.ParseRequestID(state.input.exchangeID)
	if err != nil {
		logInflightEvidenceFailure(state.input.exchangeID, err)
		return
	}
	route, err := trafficevidence.NewRoute(attempt.target.TargetID, state.input.request.Model())
	if err != nil {
		logInflightEvidenceFailure(state.input.exchangeID, err)
		return
	}
	timing := trafficevidence.NewUnknownTiming()
	if state.input.timing != nil {
		timing = *state.input.timing
	}
	event, err := trafficevidence.NewProviderInflightTrafficEvent(trafficevidence.TrafficEventInput{
		RequestID:             requestID,
		Workspace:             state.input.workspace.Slug().String(),
		ClientHandler:         state.input.clientHandler,
		ClientFamily:          trafficevidence.ClientFamily(state.input.clientFamily),
		RequestPath:           state.input.requestPath,
		Route:                 route,
		Timing:                timing,
		ModelRequested:        state.input.request.Model(),
		ModelResolved:         state.input.request.Model(),
		WorkspaceRouteModelID: state.route.name.String(),
		ProviderSpec:          profile.ProviderID(attempt.target.ProviderSpec),
		ProviderModel:         attempt.target.Model,
	}, len(state.providerCallAttempts))
	if err != nil {
		logInflightEvidenceFailure(state.input.exchangeID, err)
		return
	}
	sink.Append(ctx, event)
}

func logInflightEvidenceFailure(requestID string, err error) {
	slog.Warn("traffic evidence in-flight commit failed",
		"component", "exchange",
		"event", "traffic_evidence_inflight_commit_failed",
		"request_id", requestID,
		"error", err,
	)
}

func executeCommand(ctx context.Context, cmd command) exchangeEvent {
	switch c := cmd.(type) {
	case loadCheckpointCommand:
		var record session.Checkpoint
		var resolution session.HistoryResolution
		var current bool
		var err error
		if c.explicit {
			var found bool
			record, found, err = c.store.Get(ctx, c.workspaceSlug, c.reference)
			if found {
				resolution = session.HistoryUniqueHead
				current, err = c.store.IsCurrentHead(ctx, c.workspaceSlug, record.SessionID, record.ID)
			} else {
				resolution = session.HistoryNotFound
			}
		} else {
			record, resolution, err = c.store.ResolveHeadByHistory(ctx, c.workspaceSlug, c.history)
		}
		return checkpointLoaded{record: record, resolution: resolution, current: current, err: err}
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
	case characterizeTargetFactsCommand:
		resolutions := make(map[provider.TargetFact]bool, len(c.facts))
		for _, fact := range c.facts {
			resolution := c.backend.CharacterizeTargetFact(ctx, fact)
			if resolution.Conclusive {
				resolutions[fact] = resolution.Value
			}
		}
		return targetFactsCharacterized{attemptID: c.attemptID, generation: c.generation, resolutions: resolutions}
	case callMCPCommand:
		result, err := c.run.Call(ctx, c.call)
		return mcpToolReturned{result: result, err: err}
	case beginMCPBatchCommand:
		return mcpBatchStarted{err: c.run.BeginBatch(c.calls)}
	default:
		panic(fmt.Sprintf("exchange invariant: unsupported closed command %T", cmd))
	}
}

// terminalRoutingEvidence is the bounded routing-recovery summary carried from
// the exchange state machine to the terminal traffic event. It holds no content
// or identity. The candidate count is consumed here only to derive whether a
// fallback recovered the request; it is not retained (the terminal event needs
// the recovery fact, not the candidate tally).
type terminalRoutingEvidence struct {
	providerCallCount          int
	fallbackRecovered          bool
	possibleDuplicateExecution bool
}

// summarizeRoutingEvidence derives the recovery fact from the contiguous route
// candidates evaluated by deterministic selection and codec lowering: a request
// completed after more than one candidate was a fallback recovery.
func summarizeRoutingEvidence(attempts []providerCallAttempt, candidateCount int, completed bool) terminalRoutingEvidence {
	summary := terminalRoutingEvidence{
		providerCallCount: len(attempts),
	}
	summary.fallbackRecovered = completed && candidateCount > 1
	for index, attempt := range attempts {
		if index+1 < len(attempts) && attempt.failure != nil &&
			attempt.failure.Attempt.Execution() == provider.ExecutionMayHaveOccurred {
			summary.possibleDuplicateExecution = true
			break
		}
	}
	return summary
}

func terminalRequestOutput(input exchangeInput, response ClientResponse, target provider.TargetSnapshot, routeName routing.RouteName, routing terminalRoutingEvidence, reusablePrefix trafficevidence.ReusablePrefixEvidence) RequestOutput {
	var evidence *TrafficEvidenceInput
	if target.TargetID != "" {
		evidence = &TrafficEvidenceInput{workspace: input.workspace, routeName: routeName, exchangeID: input.exchangeID, clientHandler: input.clientHandler, clientFamily: input.clientFamily, requestPath: input.requestPath, request: input.request.Clone(), target: target, response: response, routing: routing, reusablePrefix: reusablePrefix}
	}
	return RequestOutput{Response: response, Target: target, TrafficEvidence: evidence, Compatibility: responseCompletion(response), AttemptCount: routing.providerCallCount}
}

func terminalReusablePrefix(state exchangeState) trafficevidence.ReusablePrefixEvidence {
	if len(state.providerCallAttempts) > 0 && state.providerCallAttempts[len(state.providerCallAttempts)-1].nativePreviousResponse {
		return trafficevidence.NativeReusablePrefix()
	}
	return state.reusablePrefix
}
