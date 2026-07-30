package exchange

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/mcp"
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
			request:            canonical.CloneCanonicalRequest(decoded.Request),
			rebasedRequest:     rebased,
			requestFingerprint: decoded.RequestFingerprint,
			mcpAccess:          decoded.MCPAccess,
			workspace:          workspace, timing: timing,
			requestPath: requestPath,
		},
		swobuResponseID:  responseID,
		phase:            startingPhase{},
		effectiveChanges: compat.CloneChanges(ingressChanges),
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
			return terminalRequestOutput(s.input, p.response, p.target, s.route.name, summarizeRoutingEvidence(s.providerCallAttempts, s.evaluatedCandidateCount, true)), nil
		case failedPhase:
			return terminalRequestOutput(s.input, nil, p.target, s.route.name, summarizeRoutingEvidence(s.providerCallAttempts, s.evaluatedCandidateCount, false)), p.problem
		}
		if tr.command == nil {
			return RequestOutput{}, canonical.InternalError(fmt.Sprintf("exchange invariant: active phase %T produced no command", s.phase))
		}
		current = executeCommand(ctx, tr.command)
	}
	return RequestOutput{}, canonical.InternalError("exchange transition limit reached")
}

func executeCommand(ctx context.Context, cmd command) exchangeEvent {
	switch c := cmd.(type) {
	case loadCheckpointCommand:
		var match session.HistoryMatch
		var err error
		if c.explicit {
			record, found, getErr := c.store.Get(ctx, c.workspaceSlug, c.reference)
			err = getErr
			if found {
				match = session.UniqueHistoryMatch(record)
			} else {
				match = session.MissingHistoryMatch()
			}
		} else {
			match, err = c.store.FindByHistory(ctx, c.workspaceSlug, c.history)
		}
		return checkpointLoaded{match: match, err: err}
	case materializeAttemptImagesCommand:
		preparationCtx := ctx
		cancel := func() {}
		if timeout := c.policy.TotalPreparationTimeout(); timeout > 0 {
			preparationCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		defer cancel()
		request, fetchCache, usedMedia, err := materializeRequestImages(preparationCtx, c.request, c.policy, c.limits, c.fetcher, c.fetchCache, c.historical)
		if err == nil {
			usedMedia, err = rebaseAttemptMedia(session.ResolvedRequest{Full: c.semantic}, c.request, usedMedia)
			if err != nil {
				err = canonical.InternalError("materialized media coordinates do not match the semantic request: " + err.Error())
			}
		}
		return attemptImagesMaterialized{selection: c.selection, request: request, fetchCache: fetchCache, usedMedia: usedMedia, err: err}
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

func terminalRequestOutput(input exchangeInput, response ClientResponse, target provider.TargetSnapshot, routeName routing.RouteName, routing terminalRoutingEvidence) RequestOutput {
	var evidence *TrafficEvidenceInput
	if target.TargetID != "" {
		evidence = &TrafficEvidenceInput{workspace: input.workspace, routeName: routeName, exchangeID: input.exchangeID, clientHandler: input.clientHandler, clientFamily: input.clientFamily, requestPath: input.requestPath, request: input.request.Clone(), target: target, response: response, routing: routing}
	}
	return RequestOutput{Response: response, Target: target, TrafficEvidence: evidence, Compatibility: responseCompletion(response)}
}
