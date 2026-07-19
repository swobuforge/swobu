package exchange

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

func runExchange(
	ctx context.Context,
	runner runtimeBundle,
	exchangeID string,
	clientHandler trafficevidence.ClientHandler,
	clientFamily canonical.ClientFamily,
	clientDelivery delivery.Delivery,
	request canonical.CanonicalRequest,
	workspace routing.Workspace,
	timing *trafficevidence.Timing,
) (RequestOutput, error) {
	if err := validateReplayRuntime(runner); err != nil {
		return RequestOutput{}, err
	}
	responseID, err := allocateSwobuResponseID(ctx, exchangeID, runner.SwobuResponseIDs)
	if err != nil {
		return RequestOutput{}, err
	}
	s := exchangeState{
		input: exchangeInput{
			exchangeID: exchangeID, clientHandler: clientHandler,
			clientFamily: clientFamily, clientDelivery: clientDelivery,
			request: canonical.CloneCanonicalRequest(request), workspace: workspace, timing: timing,
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
	case loadReplayCommand:
		record, found, err := c.store.Get(ctx, c.workspaceSlug, c.reference)
		return replayLoaded{record: record, found: found, err: err}
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
