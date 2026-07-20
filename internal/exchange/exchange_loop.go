package exchange

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
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
	workspace routing.Workspace,
	timing *trafficevidence.Timing,
) (RequestOutput, error) {
	if err := validateCheckpointRuntime(runner); err != nil {
		return RequestOutput{}, err
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
			workspace:          workspace, timing: timing,
		},
		swobuResponseID: responseID,
		phase:           startingPhase{},
	}
	var current exchangeEvent = exchangeStarted{}
	for steps := 0; steps < 1000; steps++ {
		tr, reduceErr := reduce(ctx, s, current, runner)
		if reduceErr != nil {
			return RequestOutput{}, canonical.InternalError(reduceErr.Error())
		}
		recordExchangeEvidenceBestEffort(ctx, runner.DecisionSink, exchangeID, tr.evidence)
		s = tr.nextState
		switch p := s.phase.(type) {
		case completedPhase:
			return terminalRequestOutput(s.input, p.response, p.target, s.route.name, len(s.providerCallAttempts)), nil
		case failedPhase:
			return terminalRequestOutput(s.input, nil, p.target, s.route.name, len(s.providerCallAttempts)), p.problem
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
	case prepareProviderAttemptCommand:
		preparationCtx := ctx
		cancel := func() {}
		if timeout := c.policy.TotalPreparationTimeout(); timeout > 0 {
			preparationCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		defer cancel()
		request, fetchCache, usedMedia, decisions, err := prepareImages(preparationCtx, c.request, c.protocol, c.policy, c.limits, c.fetcher, c.fetchCache, c.historical)
		if err == nil {
			usedMedia, err = rebaseAttemptMedia(session.ResolvedRequest{Full: c.semantic}, c.request, usedMedia)
		}
		return providerAttemptPrepared{selection: c.selection, request: request, fetchCache: fetchCache, usedMedia: usedMedia, decisions: decisions, err: err}
	case callProviderCommand:
		ingress, err := c.backend.Transport.Send(ctx, c.document)
		if err != nil {
			return providerCallFailed{attemptID: c.attemptID, err: err}
		}
		return providerIngressReceived{attemptID: c.attemptID, ingress: ingress}
	default:
		panic(fmt.Sprintf("exchange invariant: unsupported closed command %T", cmd))
	}
}

func terminalRequestOutput(input exchangeInput, response ClientResponse, target provider.TargetSnapshot, routeName routing.RouteName, providerCallCount int) RequestOutput {
	var evidence *TrafficEvidenceInput
	if target.TargetID != "" {
		evidence = &TrafficEvidenceInput{workspace: input.workspace, routeName: routeName, exchangeID: input.exchangeID, clientHandler: input.clientHandler, clientFamily: input.clientFamily, request: input.request.Clone(), target: target, response: response, providerCallAttemptCount: providerCallCount}
	}
	return RequestOutput{Response: response, Target: target, TrafficEvidence: evidence}
}
