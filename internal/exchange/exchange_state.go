package exchange

import (
	"context"
	"fmt"

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
	input      exchangeInput
	responseID replay.ResponseID
	prepared   *replay.Prepared
	route      routeCursor
	phase      phase
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

type routeCursor struct {
	attempts []routing.Attempt
	index    int
}

func newRouteCursor(attempts []routing.Attempt) routeCursor {
	return routeCursor{attempts: append([]routing.Attempt(nil), attempts...)}
}

func (r routeCursor) current() (routing.Attempt, bool) {
	if r.index < 0 || r.index >= len(r.attempts) {
		return routing.Attempt{}, false
	}
	return r.attempts[r.index], true
}

func (r routeCursor) advance() routeCursor {
	r.index++
	return r
}

type phase interface{ isPhase() }

type startingPhase struct{}

func (startingPhase) isPhase() {}

type loadingReplayPhase struct {
	reference replay.ID
}

func (loadingReplayPhase) isPhase() {}

type callingProviderPhase struct {
	attempt routing.Attempt
	call    preparedProviderCall
}

func (callingProviderPhase) isPhase() {}

type responseReturnedPhase struct {
	response ClientResponse
	target   provider.TargetSnapshot
	attempt  routing.Attempt
	count    int
}

func (responseReturnedPhase) isPhase() {}

type exchangeFailedPhase struct {
	problem error
	target  provider.TargetSnapshot
	attempt routing.Attempt
	count   int
}

func (exchangeFailedPhase) isPhase() {}

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

type providerReturned struct {
	ingress provider.Ingress
	err     error
}

func (providerReturned) isExchangeEvent() {}

type command interface{ isCommand() }

type loadReplayCommand struct {
	store         replay.Store
	workspaceSlug string
	reference     replay.ID
}

func (loadReplayCommand) isCommand() {}

// callProviderCommand is the irreducible provider I/O operation. Its document
// is final; the handler may only invoke the selected backend transport.
type callProviderCommand struct {
	backend  provider.Backend
	document carrier.Document
}

func (callProviderCommand) isCommand() {}

type preparedProviderCall struct {
	backend         provider.Backend
	request         provider.Request
	document        carrier.Document
	clientCodec     ClientCodec
	clientDelivery  delivery.Delivery
	exchangeID      string
	workspaceSlug   string
	semanticRequest canonical.CanonicalRequest
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
		return reduceStart(s, event, runner)
	case loadingReplayPhase:
		return reduceReplayLoaded(s, p, event, runner)
	case callingProviderPhase:
		return reduceProviderReturned(ctx, s, p, event, runner)
	case responseReturnedPhase, exchangeFailedPhase:
		return reducerOutcome{}, fmt.Errorf("exchange invariant: terminal phase %T received event %T", p, event)
	default:
		return reducerOutcome{}, fmt.Errorf("exchange invariant: unknown phase %T", s.phase)
	}
}

func reduceStart(s exchangeState, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	if _, ok := event.(exchangeStarted); !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: starting phase received %T", event)
	}
	route, err := s.input.workspace.ResolveRoute(s.input.request.Model())
	if err != nil {
		s.phase = exchangeFailedPhase{problem: canonical.BadRequest(err.Error())}
		return reducerOutcome{nextState: s}, nil
	}
	trace := &routing.Trace{ExchangeID: s.input.exchangeID, Workspace: s.input.workspace.Slug().String()}
	s.route = newRouteCursor(routing.BuildPlan(s.input.exchangeID, s.input.workspace.Slug(), route, trace))
	if reference, ok := replay.PreviousID(s.input.request); ok {
		s.phase = loadingReplayPhase{reference: reference}
		return reducerOutcome{nextState: s, command: loadReplayCommand{
			store: runner.ReplayStore, workspaceSlug: s.input.workspace.Slug().String(), reference: reference,
		}}, nil
	}
	prepared := replay.PrepareCurrent(s.input.request)
	s.prepared = &prepared
	return beginProviderCall(s, runner)
}

func reduceReplayLoaded(s exchangeState, phase loadingReplayPhase, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	loaded, ok := event.(replayLoaded)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: loading replay received %T", event)
	}
	if loaded.err != nil {
		s.phase = exchangeFailedPhase{problem: loaded.err}
		return reducerOutcome{nextState: s}, nil
	}
	if !loaded.found || loaded.record.ID != phase.reference {
		s.phase = exchangeFailedPhase{problem: canonical.BadRequest("unknown previous_response_id")}
		return reducerOutcome{nextState: s}, nil
	}
	prepared, err := replay.PrepareFromRecord(s.input.request, loaded.record)
	if err != nil {
		s.phase = exchangeFailedPhase{problem: err}
		return reducerOutcome{nextState: s}, nil
	}
	s.prepared = &prepared
	return beginProviderCall(s, runner)
}

func reduceProviderReturned(ctx context.Context, s exchangeState, phase callingProviderPhase, event exchangeEvent, runner runtimeBundle) (reducerOutcome, error) {
	returned, ok := event.(providerReturned)
	if !ok {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: calling provider received %T", event)
	}
	if returned.err != nil {
		evidence := exchangeEvidence{decisions: backendErrorShapeDecisions(phase.call, returned.err)}
		return reduceProviderFailure(s, phase, phase.call.backend.Target, fmt.Errorf("provider call: %w", returned.err), evidence, runner)
	}
	response, decisions, err := completeProviderCall(ctx, phase.call, returned.ingress, s.responseID, runner)
	if err != nil {
		return reduceProviderFailure(s, phase, phase.call.backend.Target, err, exchangeEvidence{decisions: decisions}, runner)
	}
	s.phase = responseReturnedPhase{response: response, target: phase.call.backend.Target, attempt: phase.attempt, count: s.route.index + 1}
	return reducerOutcome{nextState: s, evidence: exchangeEvidence{decisions: decisions}}, nil
}

func reduceProviderFailure(s exchangeState, phase callingProviderPhase, target provider.TargetSnapshot, err error, evidence exchangeEvidence, runner runtimeBundle) (reducerOutcome, error) {
	next := s.route.advance()
	if fallbackEligibleFailure(err) {
		if _, ok := next.current(); ok {
			s.route = next
			outcome, prepareErr := beginProviderCall(s, runner)
			outcome.evidence = evidence.append(outcome.evidence)
			return outcome, prepareErr
		}
	}
	s.phase = exchangeFailedPhase{problem: err, target: target, attempt: phase.attempt, count: s.route.index + 1}
	return reducerOutcome{nextState: s, evidence: evidence}, nil
}

func beginProviderCall(s exchangeState, runner runtimeBundle) (reducerOutcome, error) {
	if s.prepared == nil {
		return reducerOutcome{}, fmt.Errorf("exchange invariant: provider preparation requires loaded replay state")
	}
	attempt, ok := s.route.current()
	if !ok {
		s.phase = exchangeFailedPhase{problem: canonical.BadEndpoint("no viable provider attempt")}
		return reducerOutcome{nextState: s}, nil
	}
	call, target, evidence, err := prepareProviderCall(s, attempt, runner)
	if err != nil {
		return reduceProviderFailure(s, callingProviderPhase{attempt: attempt}, target, err, evidence, runner)
	}
	s.phase = callingProviderPhase{attempt: attempt, call: call}
	return reducerOutcome{nextState: s, command: callProviderCommand{backend: call.backend, document: call.document}, evidence: evidence}, nil
}
