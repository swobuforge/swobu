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
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/session"
)

// exchangeState is the complete control truth for one exchange. Only reduce
// replaces this value; external command handlers receive immutable commands.
type exchangeState struct {
	input                exchangeInput
	swobuResponseID      canonical.SwobuResponseID
	prepared             *session.ResolvedRequest
	mediaFetchCache      mediaFetchCache
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

type loadingCheckpointPhase struct {
	reference canonical.SwobuResponseID
}

func (loadingCheckpointPhase) isPhase() {}

type preparingProviderAttemptPhase struct {
	selection providerCallSelection
	target    provider.TargetSnapshot
}

func (preparingProviderAttemptPhase) isPhase() {}

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

type checkpointLoaded struct {
	record session.Checkpoint
	found  bool
	err    error
}

func (checkpointLoaded) isExchangeEvent() {}

type providerAttemptPrepared struct {
	selection  providerCallSelection
	request    canonical.CanonicalRequest
	decisions  []compat.Decision
	fetchCache mediaFetchCache
	usedMedia  session.ResolvedMedia
	err        error
}

func (providerAttemptPrepared) isExchangeEvent() {}

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

type loadCheckpointCommand struct {
	store         session.Store
	workspaceSlug string
	reference     canonical.SwobuResponseID
}

func (loadCheckpointCommand) isCommand() {}

type prepareProviderAttemptCommand struct {
	selection  providerCallSelection
	request    canonical.CanonicalRequest
	semantic   canonical.CanonicalRequest
	protocol   protocolkind.ProtocolKind
	policy     provider.ImageFetchPolicy
	limits     provider.MediaLimits
	fetcher    provider.ImageFetcher
	fetchCache mediaFetchCache
	historical session.ResolvedMedia
}

func (prepareProviderAttemptCommand) isCommand() {}

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
	fullRequest    canonical.CanonicalRequest
	resolvedMedia  session.ResolvedMedia
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
	case loadingCheckpointPhase:
		return reduceLoadingCheckpoint(s, p, event, runner)
	case preparingProviderAttemptPhase:
		return reducePreparingProviderAttempt(s, p, event, runner)
	case callingProviderPhase:
		return reduceCallingProvider(ctx, s, p, event, runner)
	case completedPhase, failedPhase:
		return reducerOutcome{}, fmt.Errorf("exchange invariant: terminal phase %T received event %T", p, event)
	default:
		return reducerOutcome{}, fmt.Errorf("exchange invariant: unknown phase %T", s.phase)
	}
}

func reducePreparingProviderAttempt(s exchangeState, phase preparingProviderAttemptPhase, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	prepared, ok := event.(providerAttemptPrepared)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: preparing provider attempt received %T", event)
	}
	if prepared.selection != phase.selection {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: prepared provider selection changed")
	}
	if prepared.err != nil {
		s.mediaFetchCache = cloneMediaFetchCache(prepared.fetchCache)
		if preparationErrorScope(prepared.err) == PreparationCandidate {
			next := providerCallSelection{candidateIndex: phase.selection.candidateIndex + 1, requestChoice: providerRequestPreferred}
			if _, exists := s.route.at(next.candidateIndex); exists {
				outcome, err := advanceProviderExecutionFrom(s, runner, next)
				outcome.evidence = exchangeEvidence{decisions: prepared.decisions}.append(outcome.evidence)
				return outcome, err
			}
		}
		s.phase = failedPhase{problem: prepared.err, target: phase.target}
		return reducerOutcome{nextState: s, evidence: exchangeEvidence{decisions: prepared.decisions}}, nil
	}
	s.mediaFetchCache = cloneMediaFetchCache(prepared.fetchCache)
	call, target, evidence, preparation, err := prepareProviderCall(s, phase.selection, runner, &prepared)
	evidence.decisions = append(prepared.decisions, evidence.decisions...)
	if preparation != nil {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: prepared request requested media preparation again")
	}
	if err != nil {
		if preparationErrorScope(err) == PreparationCandidate {
			next := providerCallSelection{candidateIndex: phase.selection.candidateIndex + 1, requestChoice: providerRequestPreferred}
			if _, exists := s.route.at(next.candidateIndex); exists {
				outcome, advanceErr := advanceProviderExecutionFrom(s, runner, next)
				outcome.evidence = evidence.append(outcome.evidence)
				return outcome, advanceErr
			}
		}
		s.phase = failedPhase{problem: err, target: target}
		return reducerOutcome{nextState: s, evidence: evidence}, nil
	}
	return beginProviderCallAttempt(s, phase.selection, call, evidence)
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
	if reference, ok, err := session.PreviousSwobuResponseID(s.input.request); err != nil {
		s.phase = failedPhase{problem: err}
		return reducerOutcome{nextState: s}, nil
	} else if ok {
		s.phase = loadingCheckpointPhase{reference: reference}
		return reducerOutcome{nextState: s, command: loadCheckpointCommand{
			store: runner.CheckpointStore, workspaceSlug: s.input.workspace.Slug().String(), reference: reference,
		}}, nil
	}
	prepared, err := session.Begin(s.input.request)
	if err != nil {
		s.phase = failedPhase{problem: err}
		return reducerOutcome{nextState: s}, nil
	}
	s.prepared = &prepared
	return advanceProviderExecution(s, runner)
}

func reduceLoadingCheckpoint(s exchangeState, phase loadingCheckpointPhase, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	loaded, ok := event.(checkpointLoaded)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: loading checkpoint received %T", event)
	}
	if loaded.err != nil {
		s.phase = failedPhase{problem: loaded.err}
		return reducerOutcome{nextState: s}, nil
	}
	if !loaded.found {
		s.phase = failedPhase{problem: canonical.BadRequest("unknown previous_response_id")}
		return reducerOutcome{nextState: s}, nil
	}
	prepared, err := session.Resume(s.input.request, loaded.record)
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
		return reducerOutcome{}, fmt.Errorf("exchange invariant: provider preparation requires resolved session request")
	}
	selection, ok := selectNextProviderCall(s)
	if !ok {
		return terminateProviderExecution(s), nil
	}
	return advanceProviderExecutionFrom(s, runner, selection)
}

func advanceProviderExecutionFrom(s exchangeState, runner runtimeBundle, selection providerCallSelection) (reducerOutcome, error) {
	evidence := exchangeEvidence{}
	for {
		call, target, preparedEvidence, preparation, err := prepareProviderCall(s, selection, runner, nil)
		evidence = evidence.append(preparedEvidence)
		if preparation != nil {
			s.phase = preparingProviderAttemptPhase{selection: selection, target: target}
			return reducerOutcome{nextState: s, command: *preparation, evidence: evidence}, nil
		}
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
func previousResponseHandoffEvidence(prepared session.ResolvedRequest, winner providerCallAttempt, attemptID providerCallAttemptID) []compat.Decision {
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
