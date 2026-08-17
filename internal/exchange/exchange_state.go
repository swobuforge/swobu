package exchange

import (
	"context"
	"errors"
	"fmt"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/cachelocality"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/wire"
)

// exchangeState is the complete control truth for one exchange. Only reduce
// replaces this value; external command handlers receive immutable commands.
type exchangeState struct {
	input                   exchangeInput
	swobuResponseID         canonical.SwobuResponseID
	draft                   *session.Draft
	prepared                *session.ResolvedRequest
	mediaFetchCache         mediaFetchCache
	route                   routePlan
	providerCallAttempts    []providerCallAttempt
	evaluatedCandidateCount int
	phase                   phase
	advance                 *historyAdvance
	sessionID               session.ClientSessionID
	expectedHead            canonical.SwobuResponseID
	mcp                     *mcp.Run
	providerUsage           []canonical.TokenUsage
	effectiveChanges        []compat.Change
	cacheLocality           cachelocality.Key
	reusablePrefix          trafficevidence.ReusablePrefixEvidence
	previousRequest         *canonical.CanonicalRequest
	polyfilled              bool
}

// historyAdvance is the complete optional input for composing the completed
// exchange into client-visible history. Nil means composition is unavailable;
// a nil Previous inside a present value denotes genesis.
type historyAdvance struct {
	Previous *historyfingerprint.History
	Request  historyfingerprint.Request
}

type exchangeInput struct {
	exchangeID            string
	clientHandler         trafficevidence.ClientHandler
	clientFamily          canonical.ClientFamily
	clientDelivery        delivery.Delivery
	request               canonical.CanonicalRequest
	rebasedRequest        *wire.RebasedRequest
	requestFingerprint    historyfingerprint.Request
	mcpAccess             mcp.Access
	explicitCacheLocality cachelocality.Key
	workspace             routing.Workspace
	timing                *trafficevidence.Timing
	// requestPath is the ingress-owned normalized path, captured before the
	// exchange runs so terminal evidence is complete on both success and failure.
	requestPath canonical.NormalizedPath
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
	explicit  bool
	reference canonical.SwobuResponseID
	history   historyfingerprint.History
	scheme    historyfingerprint.Scheme
}

func (loadingCheckpointPhase) isPhase() {}

type preparingMCPPhase struct{}

func (preparingMCPPhase) isPhase() {}

type callingProviderPhase struct {
	attemptID providerCallAttemptID
	call      providerCall
}

type callingMCPPhase struct {
	selection providerCallSelection
	target    provider.TargetSnapshot
	response  canonical.CanonicalResponse
	calls     []canonical.ToolCallItem
	results   []canonical.CanonicalItem
	next      int
}

func (callingMCPPhase) isPhase() {}

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
	record     session.Checkpoint
	resolution session.HistoryResolution
	current    bool
	err        error
}

func (checkpointLoaded) isExchangeEvent() {}

type mcpPrepared struct {
	full    canonical.CanonicalRequest
	run     *mcp.Run
	changes []compat.Change
	err     error
}

func (mcpPrepared) isExchangeEvent() {}

type mcpBatchStarted struct{ err error }

func (mcpBatchStarted) isExchangeEvent() {}

type providerIngressReceived struct {
	attemptID providerCallAttemptID
	ingress   provider.Ingress
}

func (providerIngressReceived) isExchangeEvent() {}

type providerCallFailed struct {
	attemptID providerCallAttemptID
	failure   provider.AttemptFailure
}

type mcpToolReturned struct {
	result canonical.CanonicalItem
	err    error
}

func (mcpToolReturned) isExchangeEvent() {}

func (providerCallFailed) isExchangeEvent() {}

type command interface{ isCommand() }

type loadCheckpointCommand struct {
	store         session.Store
	workspaceSlug string
	explicit      bool
	reference     canonical.SwobuResponseID
	history       historyfingerprint.History
	scheme        historyfingerprint.Scheme
}

func (loadCheckpointCommand) isCommand() {}

type prepareMCPCommand struct {
	full   canonical.CanonicalRequest
	access mcp.Access
}

func (prepareMCPCommand) isCommand() {}

// callProviderCommand is the irreducible provider I/O operation. Its document
// is final; the handler may only invoke the selected backend transport.
type callProviderCommand struct {
	attemptID providerCallAttemptID
	backend   provider.Backend
	document  carrier.Document
}

type callMCPCommand struct {
	run  *mcp.Run
	call canonical.ToolCallItem
}

func (callMCPCommand) isCommand() {}

type beginMCPBatchCommand struct {
	run   *mcp.Run
	calls []canonical.ToolCallItem
}

func (beginMCPBatchCommand) isCommand() {}

func (callProviderCommand) isCommand() {}

// providerCall is the complete immutable data needed to issue and finish one
// provider call. It contains no alternative request or retry state.
type providerCall struct {
	backend            provider.Backend
	request            provider.Request
	document           carrier.Document
	clientCodec        ClientCodec
	clientDelivery     delivery.Delivery
	exchangeID         string
	workspaceSlug      string
	fullRequest        canonical.CanonicalRequest
	decodeContext      canonical.CanonicalRequest
	inputSegment       canonical.CanonicalRequest
	historyScheme      historyfingerprint.Scheme
	advance            *historyAdvance
	sessionID          session.ClientSessionID
	expectedHead       canonical.SwobuResponseID
	delayClientHandoff bool
	providerRound      int
}

type reducerOutcome struct {
	nextState exchangeState
	command   command
}

func reduce(ctx context.Context, s exchangeState, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	switch p := s.phase.(type) {
	case startingPhase:
		return reduceStarting(s, event, runner)
	case loadingCheckpointPhase:
		return reduceLoadingCheckpoint(s, p, event, runner)
	case preparingMCPPhase:
		return reducePreparingMCP(ctx, s, event, runner)
	case callingProviderPhase:
		return reduceCallingProvider(ctx, s, p, event, runner)
	case callingMCPPhase:
		return reduceCallingMCP(ctx, s, p, event, runner)
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
	var err error
	s, err = applyRoutePlan(s, s.swobuResponseID.String())
	if err != nil {
		s.phase = failedPhase{problem: canonical.BadRequest(err.Error())}
		return reducerOutcome{nextState: s}, nil
	}
	if reference, ok, err := session.PreviousSwobuResponseID(s.input.request); err != nil {
		s.phase = failedPhase{problem: err}
		return reducerOutcome{nextState: s}, nil
	} else if ok {
		s.phase = loadingCheckpointPhase{explicit: true, reference: reference, scheme: s.input.requestFingerprint.Scheme()}
		return reducerOutcome{nextState: s, command: loadCheckpointCommand{
			store: runner.CheckpointStore, workspaceSlug: s.input.workspace.Slug().String(), explicit: true, reference: reference,
			scheme: s.input.requestFingerprint.Scheme(),
		}}, nil
	}
	if s.input.rebasedRequest != nil {
		s.phase = loadingCheckpointPhase{history: s.input.rebasedRequest.Previous}
		return reducerOutcome{nextState: s, command: loadCheckpointCommand{
			store: runner.CheckpointStore, workspaceSlug: s.input.workspace.Slug().String(), history: s.input.rebasedRequest.Previous,
		}}, nil
	}
	draft, err := session.PrepareBegin(s.input.request)
	if err != nil {
		s.phase = failedPhase{problem: err}
		return reducerOutcome{nextState: s}, nil
	}
	s.draft = &draft
	s.advance = &historyAdvance{Request: s.input.requestFingerprint}
	return beginMCPPreparation(s, runner)
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
	if phase.explicit && loaded.resolution == session.HistoryNotFound {
		s.phase = failedPhase{problem: canonical.BadRequest("unknown previous_response_id")}
		return reducerOutcome{nextState: s}, nil
	}
	if phase.explicit && (!loaded.current || loaded.record.HistoryScheme != phase.scheme) {
		s.phase = failedPhase{problem: canonical.BadRequest("previous_response_id is not the current checkpoint for this client codec")}
		return reducerOutcome{nextState: s}, nil
	}
	record := loaded.record
	found := loaded.resolution == session.HistoryUniqueHead
	var draft session.Draft
	var err error
	if phase.explicit {
		draft, err = session.PrepareResume(s.input.request, record)
		if record.History != nil {
			history := *record.History
			s.advance = &historyAdvance{Previous: &history, Request: s.input.requestFingerprint}
		}
	} else if found {
		draft, err = session.PrepareResume(s.input.rebasedRequest.Request, record)
		history := phase.history
		s.advance = &historyAdvance{Previous: &history, Request: s.input.requestFingerprint}
	} else {
		draft, err = session.PrepareBegin(s.input.request)
		history := phase.history
		s.advance = &historyAdvance{Previous: &history, Request: s.input.requestFingerprint}
	}
	if err != nil {
		s.phase = failedPhase{problem: err}
		return reducerOutcome{nextState: s}, nil
	}
	s.draft = &draft
	if found || phase.explicit {
		s.sessionID = record.SessionID
		s.expectedHead = record.ID
		previous := record.Request.Clone()
		s.previousRequest = &previous
		if s.input.explicitCacheLocality.IsZero() {
			s, err = applyRoutePlan(s, string(record.SessionID))
			if err != nil {
				s.phase = failedPhase{problem: canonical.BadRequest(err.Error())}
				return reducerOutcome{nextState: s}, nil
			}
		}
	}
	return beginMCPPreparation(s, runner)
}

// applyRoutePlan makes effective cache locality one reducer-owned fact consumed
// by target ordering and every provider attempt. It is a stable preference;
// normal route fallback remains available when the preferred target fails.
func applyRoutePlan(s exchangeState, lineage string) (exchangeState, error) {
	route, err := s.input.workspace.ResolveRoute(s.input.request.Model())
	if err != nil {
		return s, err
	}
	locality := s.input.explicitCacheLocality
	if locality.IsZero() {
		locality = cachelocality.Derived(s.input.workspace.Slug().String(), lineage)
	}
	s.cacheLocality = locality
	s.route = newRoutePlan(route.Name(), routing.BuildPlan(locality.Key(), route))
	return s, nil
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
		var err error
		s, err = failProviderCallAttempt(s, phase.attemptID, result.failure)
		if err != nil {
			return reducerOutcome{}, err
		}
		outcome, err := advanceProviderExecution(ctx, s, runner)
		return outcome, err

	case providerIngressReceived:
		if result.attemptID != phase.attemptID {
			return reducerOutcome{}, fmt.Errorf("exchange invariant: provider call attempt %d returned while %d is active", result.attemptID, phase.attemptID)
		}
		response, canonicalResponse, providerCompatibility, completionErr := completeProviderCall(ctx, phase.call, result.ingress, s.swobuResponseID, runner)
		changes := providerCompatibility.completedChanges()
		if completionErr != nil {
			var err error
			s, err = failProviderCallAttempt(s, phase.attemptID, provider.AttemptMayHaveExecuted(completionErr))
			if err != nil {
				return reducerOutcome{}, err
			}
			outcome, err := advanceProviderExecution(ctx, s, runner)
			return outcome, err
		}
		var err error
		s, err = completeProviderCallAttempt(s, phase.attemptID)
		if err != nil {
			return reducerOutcome{}, err
		}
		if canonicalResponse != nil {
			s.effectiveChanges = append(s.effectiveChanges, attempt.requestChanges...)
			s.effectiveChanges = append(s.effectiveChanges, changes...)
			rounds := append(append([]canonical.TokenUsage(nil), s.providerUsage...), canonicalResponse.Usage())
			calls, err := s.mcp.Calls(*canonicalResponse)
			if err != nil {
				s.phase = failedPhase{problem: err, target: attempt.target}
				return reducerOutcome{nextState: s}, nil
			}
			if len(calls) > 0 {
				s.providerUsage = rounds
				selection := providerCallSelection{candidateIndex: attempt.candidateIndex, requestChoice: providerRequestPreferred}
				mcpPhase := callingMCPPhase{
					selection: selection, target: attempt.target, calls: calls,
					response: canonicalResponse.Clone(),
				}
				outcome, beginErr := beginMCPBatch(s, mcpPhase)
				return outcome, beginErr
			}
			*canonicalResponse = canonicalResponse.WithUsage(canonical.SumTokenUsage(rounds...))
			response, err = handoffCompletedProviderResponse(ctx, phase.call, *canonicalResponse, runner)
			if err != nil {
				s.phase = failedPhase{problem: err, target: attempt.target}
				return reducerOutcome{nextState: s}, nil
			}
		}
		effective := compat.CloneChanges(s.effectiveChanges)
		if canonicalResponse == nil {
			effective = append(effective, attempt.requestChanges...)
			effective = append(effective, providerCompatibility.initial...)
		}
		effective = append(effective, previousResponseHandoffEvidence(*s.prepared, attempt)...)
		if completion := responseCompletion(response); completion != nil {
			completion.ConfigureCompatibility(effective, providerCompatibility.progressive, s.polyfilled)
		}
		s.phase = completedPhase{response: response, target: attempt.target}
		return reducerOutcome{nextState: s}, nil
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
	retry          bool
}

// advanceProviderExecution is the sole transition that chooses further
// provider execution. It performs deterministic preparation but never I/O.
func advanceProviderExecution(ctx context.Context, s exchangeState, runner runtimeBundle) (reducerOutcome, error) {
	if s.prepared == nil {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: provider preparation requires resolved session request")
	}
	selection, ok := selectNextProviderCall(s)
	if !ok {
		return terminateProviderExecution(s), nil
	}
	return advanceProviderExecutionFrom(ctx, s, runner, selection)
}

func advanceProviderExecutionFrom(ctx context.Context, s exchangeState, runner runtimeBundle, selection providerCallSelection) (reducerOutcome, error) {
	for {
		if evaluated := selection.candidateIndex + 1; evaluated > s.evaluatedCandidateCount {
			s.evaluatedCandidateCount = evaluated
		}
		call, target, requestChanges, fetchCache, err := prepareProviderCall(ctx, s, selection, runner)
		s.mediaFetchCache = cloneMediaFetchCache(fetchCache)
		if err == nil {
			return beginProviderCallAttempt(s, selection, call, requestChanges)
		}
		if candidatePreparationCanAdvance(err) {
			if next, exists := nextRouteCandidate(s, selection); exists {
				selection = next
				continue
			}
			var incompatible provider.IncompatibleTargetError
			if errors.As(err, &incompatible) {
				err = noCompatibleTarget(err)
			}
		}
		s.phase = failedPhase{problem: err, target: target}
		return reducerOutcome{nextState: s}, nil
	}
}

func nextRouteCandidate(s exchangeState, selection providerCallSelection) (providerCallSelection, bool) {
	next := providerCallSelection{
		candidateIndex: selection.candidateIndex + 1,
		requestChoice:  providerRequestPreferred,
	}
	_, exists := s.route.at(next.candidateIndex)
	return next, exists
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
	if !providerRecoveryPermitted(last) {
		return providerCallSelection{}, false
	}
	if providerFailureTransient(last.failure.Attempt.Cause()) && !last.retry {
		return providerCallSelection{
			candidateIndex: last.candidateIndex,
			requestChoice:  last.requestChoice,
			retry:          true,
		}, true
	}
	if providerFailureAdvancesCandidate(last.failure.Attempt.Cause()) {
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
	problem := last.failure.Attempt.Cause()
	var incompatible provider.IncompatibleTargetError
	if errors.As(problem, &incompatible) {
		problem = noCompatibleTarget(problem)
	}
	s.phase = failedPhase{problem: problem, target: last.target}
	return reducerOutcome{nextState: s}
}

func noCompatibleTarget(lastRejection error) canonical.Error {
	message := "no configured target can represent the canonical request"
	if lastRejection != nil {
		message += ": " + lastRejection.Error()
	}
	return canonical.NoCompatibleTarget(message)
}

func nativePreviousResponseSent(request provider.Request) bool {
	return request.PreviousHistory != nil
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
	if attempt.failure == nil ||
		attempt.failure.Attempt.Execution() != provider.ExecutionRejectedBeforeExecution ||
		!attempt.nativePreviousResponse {
		return false
	}
	var rejected provider.RejectedError
	if !errors.As(attempt.failure.Attempt.Cause(), &rejected) {
		return false
	}
	return canonical.IsBackendErrorClass(
		attempt.failure.Attempt.Cause(),
		canonical.BackendErrorClassContinuationReferenceInvalid,
	)
}

func candidatePreparationCanAdvance(err error) bool {
	var incompatible provider.IncompatibleTargetError
	var unavailable provider.UnavailableError
	return errors.As(err, &incompatible) || errors.As(err, &unavailable)
}

func providerRecoveryPermitted(attempt providerCallAttempt) bool {
	if attempt.failure == nil || attempt.providerRound > 0 {
		return false
	}
	switch attempt.failure.Attempt.Execution() {
	case provider.ExecutionNotDispatched, provider.ExecutionRejectedBeforeExecution:
		return true
	case provider.ExecutionMayHaveOccurred:
		return true
	default:
		return false
	}
}

func providerFailureTransient(err error) bool {
	var unavailable provider.UnavailableError
	return errors.As(err, &unavailable)
}

func providerFailureAdvancesCandidate(err error) bool {
	var incompatible provider.IncompatibleTargetError
	var rejected provider.RejectedError
	return providerFailureTransient(err) ||
		errors.As(err, &incompatible) ||
		errors.As(err, &rejected)
}

// previousResponseHandoffEvidence describes only the winning representation.
// Failed and preparation paths do not call this function.
func previousResponseHandoffEvidence(prepared session.ResolvedRequest, winner providerCallAttempt) []compat.Change {
	if !prepared.HasPreviousHistory() || winner.nativePreviousResponse {
		return nil
	}
	return []compat.Change{compat.NewApproximation(
		canonical.RequestPreviousResponseResponses,
		canonical.RequestPreviousResponse,
		canonical.Occurrence{},
	)}
}
