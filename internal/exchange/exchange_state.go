package exchange

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/routing"
)

// exchangeState is the complete control truth for one exchange. Only reduce
// replaces this value; external command handlers receive immutable commands.
type exchangeState struct {
	input                exchangeInput
	swobuResponseID      canonical.SwobuResponseID
	prepared             *replay.Prepared
	route                routePlan
	providerCallAttempts []providerCallAttempt
	phase                phase
}

type exchangeInput struct {
	exchangeID     string
	clientHandler  trafficevidence.ClientHandler
	clientFamily   canonical.ClientFamily
	clientDelivery delivery.Delivery
	request        canonical.CanonicalRequest
	workspace      routing.Workspace
	timing         *trafficevidence.Timing
}

type routePlan struct {
	name    routing.RouteName
	targets []routing.Target
}

func newRoutePlan(name routing.RouteName, targets []routing.Target) routePlan {
	return routePlan{name: name, targets: append([]routing.Target(nil), targets...)}
}

func (r routePlan) at(index int) (routing.Target, bool) {
	if index < 0 || index >= len(r.targets) {
		return routing.Target{}, false
	}
	return r.targets[index], true
}

type phase interface{ isPhase() }

type startingPhase struct{}

func (startingPhase) isPhase() {}

type loadingReplayPhase struct {
	reference canonical.SwobuResponseID
}

func (loadingReplayPhase) isPhase() {}

type callingProviderPhase struct {
	attemptID providerCallAttemptID
	call      providerCall
}

func (callingProviderPhase) isPhase() {}

type completedPhase struct {
	response ClientResponse
	target   provider.TargetSnapshot
}

func (completedPhase) isPhase() {}

type failedPhase struct {
	problem error
	target  provider.TargetSnapshot
}

func (failedPhase) isPhase() {}

// Events are concrete facts. No tag selects a dormant payload.
type exchangeEvent interface{ isExchangeEvent() }

type exchangeStarted struct{}

func (exchangeStarted) isExchangeEvent() {}

type replayLoaded struct {
	record replay.Record
	found  bool
	err    error
}

func (replayLoaded) isExchangeEvent() {}

type providerIngressReceived struct {
	attemptID providerCallAttemptID
	ingress   provider.Ingress
}

func (providerIngressReceived) isExchangeEvent() {}

type providerCallFailed struct {
	attemptID providerCallAttemptID
	err       error
}

func (providerCallFailed) isExchangeEvent() {}

type command interface{ isCommand() }

type loadReplayCommand struct {
	store         replay.Store
	workspaceSlug string
	reference     canonical.SwobuResponseID
}

func (loadReplayCommand) isCommand() {}

// callProviderCommand is the irreducible provider I/O operation. Its document
// is final; the handler may only invoke the selected backend transport.
type callProviderCommand struct {
	attemptID providerCallAttemptID
	backend   provider.Backend
	document  carrier.Document
}

func (callProviderCommand) isCommand() {}

// providerCall is the complete immutable data needed to issue and finish one
// provider call. It contains no alternative request or retry state.
type providerCall struct {
	backend        provider.Backend
	request        provider.Request
	document       carrier.Document
	clientCodec    ClientCodec
	clientDelivery delivery.Delivery
	exchangeID     string
	workspaceSlug  string
	replayRequest  canonical.CanonicalRequest
}

type reducerOutcome struct {
	nextState exchangeState
	command   command
	evidence  exchangeEvidence
}

type exchangeEvidence struct {
	decisions []compat.Decision
}

func (e exchangeEvidence) append(other exchangeEvidence) exchangeEvidence {
	e.decisions = append(e.decisions, other.decisions...)
	return e
}

func reduce(ctx context.Context, s exchangeState, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	switch p := s.phase.(type) {
	case startingPhase:
		return reduceStarting(s, event, runner)
	case loadingReplayPhase:
		return reduceLoadingReplay(s, p, event, runner)
	case callingProviderPhase:
		return reduceCallingProvider(ctx, s, p, event, runner)
	case completedPhase, failedPhase:
		return reducerOutcome{}, fmt.Errorf("exchange invariant: terminal phase %T received event %T", p, event)
	default:
		return reducerOutcome{}, fmt.Errorf("exchange invariant: unknown phase %T", s.phase)
	}
}

func reduceStarting(s exchangeState, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	if _, ok := event.(exchangeStarted); !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: starting phase received %T", event)
	}
	route, err := s.input.workspace.ResolveRoute(s.input.request.Model())
	if err != nil {
		s.phase = failedPhase{problem: canonical.BadRequest(err.Error())}
		return reducerOutcome{nextState: s}, nil
	}
	s.route = newRoutePlan(route.Name(), routing.BuildPlan(s.input.exchangeID, route))
	if reference, ok, err := replay.PreviousSwobuResponseID(s.input.request); err != nil {
		s.phase = failedPhase{problem: err}
		return reducerOutcome{nextState: s}, nil
	} else if ok {
		s.phase = loadingReplayPhase{reference: reference}
		return reducerOutcome{nextState: s, command: loadReplayCommand{
			store: runner.ReplayStore, workspaceSlug: s.input.workspace.Slug().String(), reference: reference,
		}}, nil
	}
	prepared := replay.PrepareCurrent(s.input.request)
	s.prepared = &prepared
	return advanceProviderExecution(s, runner)
}

func reduceLoadingReplay(s exchangeState, phase loadingReplayPhase, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	loaded, ok := event.(replayLoaded)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: loading replay received %T", event)
	}
	if loaded.err != nil {
		s.phase = failedPhase{problem: loaded.err}
		return reducerOutcome{nextState: s}, nil
	}
	if !loaded.found {
		s.phase = failedPhase{problem: canonical.BadRequest("unknown previous_response_id")}
		return reducerOutcome{nextState: s}, nil
	}
	prepared, err := replay.PrepareFromRecord(s.input.request, phase.reference, loaded.record)
	if err != nil {
		s.phase = failedPhase{problem: err}
		return reducerOutcome{nextState: s}, nil
	}
	s.prepared = &prepared
	return advanceProviderExecution(s, runner)
}

func reduceCallingProvider(ctx context.Context, s exchangeState, phase callingProviderPhase, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	attempt, ok := findProviderCallAttempt(s, phase.attemptID)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: active provider call attempt %d is unknown", phase.attemptID)
	}
	switch result := event.(type) {
	case providerCallFailed:
		if result.attemptID != phase.attemptID {
			return reducerOutcome{}, fmt.Errorf("exchange invariant: provider call attempt %d returned while %d is active", result.attemptID, phase.attemptID)
		}
		if result.err == nil {
			return reducerOutcome{}, fmt.Errorf("exchange invariant: provider call attempt %d failed without an error", result.attemptID)
		}
		failure := fmt.Errorf("provider call: %w", result.err)
		var err error
		s, err = failProviderCallAttempt(s, phase.attemptID, providerCallFailureBeforeIngress, failure)
		if err != nil {
			return reducerOutcome{}, err
		}
		evidence := exchangeEvidence{decisions: backendErrorShapeDecisions(phase.call, result.err)}
		outcome, err := advanceProviderExecution(s, runner)
		outcome.evidence = evidence.append(outcome.evidence)
		return outcome, err

	case providerIngressReceived:
		if result.attemptID != phase.attemptID {
			return reducerOutcome{}, fmt.Errorf("exchange invariant: provider call attempt %d returned while %d is active", result.attemptID, phase.attemptID)
		}
		response, completionDecisions, completionErr := completeProviderCall(ctx, phase.call, result.ingress, s.swobuResponseID, runner)
		decisions := append([]compat.Decision(nil), completionDecisions...)
		if completionErr != nil {
			var err error
			s, err = failProviderCallAttempt(s, phase.attemptID, providerCallFailureBeforeHandoff, completionErr)
			if err != nil {
				return reducerOutcome{}, err
			}
			outcome, err := advanceProviderExecution(s, runner)
			outcome.evidence = exchangeEvidence{decisions: decisions}.append(outcome.evidence)
			return outcome, err
		}
		var err error
		s, err = completeProviderCallAttempt(s, phase.attemptID)
		if err != nil {
			return reducerOutcome{}, err
		}
		s.phase = completedPhase{response: response, target: attempt.target}
		decisions = append(decisions, previousResponseHandoffEvidence(*s.prepared, attempt, phase.attemptID)...)
		return reducerOutcome{nextState: s, evidence: exchangeEvidence{decisions: decisions}}, nil
	default:
		return reducerOutcome{}, fmt.Errorf("exchange invariant: calling provider received %T", event)
	}
}

type providerRequestChoice uint8

const (
	providerRequestPreferred providerRequestChoice = iota
	providerRequestFullHistory
)

type providerCallSelection struct {
	candidateIndex int
	requestChoice  providerRequestChoice
}

// advanceProviderExecution is the sole transition that chooses further
// provider execution. It performs deterministic preparation but never I/O.
func advanceProviderExecution(s exchangeState, runner runtimeBundle) (reducerOutcome, error) {
	if s.prepared == nil {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: provider preparation requires loaded replay state")
	}
	selection, ok := selectNextProviderCall(s)
	if !ok {
		return terminateProviderExecution(s), nil
	}
	evidence := exchangeEvidence{}
	for {
		call, target, preparedEvidence, err := prepareProviderCall(s, selection, runner)
		evidence = evidence.append(preparedEvidence)
		if err == nil {
			return beginProviderCallAttempt(s, selection, call, evidence)
		}
		if routeFailoverEligible(err) {
			nextIndex := selection.candidateIndex + 1
			if _, exists := s.route.at(nextIndex); exists {
				selection = providerCallSelection{candidateIndex: nextIndex, requestChoice: providerRequestPreferred}
				continue
			}
		}
		s.phase = failedPhase{problem: err, target: target}
		return reducerOutcome{nextState: s, evidence: evidence}, nil
	}
}

// selectNextProviderCall is a pure closed choice over route order, prepared
// canonical forms, and recorded call outcomes.
func selectNextProviderCall(s exchangeState) (providerCallSelection, bool) {
	if len(s.providerCallAttempts) == 0 {
		_, ok := s.route.at(0)
		return providerCallSelection{candidateIndex: 0, requestChoice: providerRequestPreferred}, ok
	}
	last := s.providerCallAttempts[len(s.providerCallAttempts)-1]
	if !last.terminal() || last.status == providerCallAttemptHandoffReady {
		return providerCallSelection{}, false
	}
	if alternative, ok := selectSameTargetAlternative(s.providerCallAttempts); ok {
		return alternative, true
	}
	if routeFailoverEligible(last.failure.Cause) {
		nextIndex := last.candidateIndex + 1
		_, ok := s.route.at(nextIndex)
		return providerCallSelection{candidateIndex: nextIndex, requestChoice: providerRequestPreferred}, ok
	}
	return providerCallSelection{}, false
}

func terminateProviderExecution(s exchangeState) reducerOutcome {
	if len(s.providerCallAttempts) == 0 {
		s.phase = failedPhase{problem: canonical.BadEndpoint("no viable provider candidate")}
		return reducerOutcome{nextState: s}
	}
	last := s.providerCallAttempts[len(s.providerCallAttempts)-1]
	s.phase = failedPhase{problem: last.failure.Cause, target: last.target}
	return reducerOutcome{nextState: s}
}

func nativePreviousResponseSent(request provider.Request) bool {
	ref, ok := request.Canonical.PreviousResponse()
	return ok && ref.Responses != nil
}

// selectSameTargetAlternative maps the issued call facts to one concrete
// transient request choice. Requirements prove the same fallback cannot recur.
func selectSameTargetAlternative(attempts []providerCallAttempt) (providerCallSelection, bool) {
	if len(attempts) == 0 {
		return providerCallSelection{}, false
	}
	last := attempts[len(attempts)-1]
	if nativeReferenceFailureAdmitsFullHistory(last) {
		return providerCallSelection{candidateIndex: last.candidateIndex, requestChoice: providerRequestFullHistory}, true
	}
	return providerCallSelection{}, false
}

func nativeReferenceFailureAdmitsFullHistory(attempt providerCallAttempt) bool {
	if attempt.failure == nil || attempt.failure.Stage != providerCallFailureBeforeIngress ||
		!attemptRequires(attempt, compat.RequestPreviousResponseResponses) {
		return false
	}
	var rejected provider.RejectedError
	if !errors.As(attempt.failure.Cause, &rejected) {
		return false
	}
	statusCode := backendStatusCode(attempt.failure.Cause)
	return statusCode == http.StatusBadRequest || statusCode == http.StatusNotFound
}

func previousResponseDecision(attempt providerCallAttempt, attemptID providerCallAttemptID, outcome compat.Outcome) compat.Decision {
	return compat.Decision{Feature: compat.RequestPreviousResponseResponses, Outcome: outcome, Subject: previousResponseSubject(attempt, attemptID)}
}

func parentPreviousResponseDecision(attempt providerCallAttempt, attemptID providerCallAttemptID, outcome compat.Outcome) compat.Decision {
	return compat.Decision{Feature: compat.RequestPreviousResponse, Outcome: outcome, Subject: previousResponseSubject(attempt, attemptID)}
}

func previousResponseSubject(attempt providerCallAttempt, attemptID providerCallAttemptID) compat.Subject {
	return compat.Subject(fmt.Sprintf(
		"route:target/%s/version/%d/provider_call/%d",
		attempt.target.TargetID,
		attempt.target.TargetVersion,
		attemptID,
	))
}

func attemptRequires(attempt providerCallAttempt, feature compat.Feature) bool {
	for _, requirement := range attempt.requirements {
		if requirement == feature {
			return true
		}
	}
	return false
}

// previousResponseHandoffEvidence describes only the winning representation.
// Failed and preparation paths do not call this function.
func previousResponseHandoffEvidence(prepared replay.Prepared, winner providerCallAttempt, attemptID providerCallAttemptID) []compat.Decision {
	previous, hasPrevious := prepared.Delta.PreviousResponse()
	if !hasPrevious || previous.Responses == nil {
		return nil
	}
	if attemptRequires(winner, compat.RequestPreviousResponseResponses) {
		return []compat.Decision{previousResponseDecision(winner, attemptID, compat.Exact)}
	}
	return []compat.Decision{
		previousResponseDecision(winner, attemptID, compat.Drop),
		parentPreviousResponseDecision(winner, attemptID, compat.Exact),
	}
}
