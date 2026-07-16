package exchange

import (
	"context"
	"errors"
	"reflect"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/machine"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/routing"
)

// runExchangeWithMachine runs one request lifecycle through the machine.
// It replaces the old exchangeGraph.Execute imperative loop.
//
// Architecture: one machine.Engine with all routing and runner reducers
// registered in a single registry, and one interpreter that handles all
// commands (routing + runner) in the same dispatch loop.
func runExchangeWithMachine(
	ctx context.Context,
	runner Runner,
	sink TrafficEventSink,
	exchangeID string,
	clientHandler trafficevidence.ClientHandler,
	clientFamily canonical.ClientFamily,
	clientDelivery delivery.Delivery,
	request canonical.CanonicalRequest,
	endpoint endpointintent.Endpoint,
	timing *trafficevidence.Timing,
) (RequestOutput, error) {
	reg := machine.NewRegistry()
	// Routing reducers
	reg.Register(planRoute)
	reg.Register(selectNextAttempt)
	reg.Register(handleProviderFailed)
	reg.Register(scheduleRetry)
	reg.Register(requestProviderSend)
	reg.Register(terminalSuccess)
	// Runner reducers
	reg.Register(pipelineStartedReduce)
	reg.Register(codecsResolvedReduce)
	reg.Register(requestEncodedReduce)
	reg.Register(ingressReceivedReduce)
	reg.Register(envelopeDecodedReduce)
	reg.Register(pipelineCompletedReduce)

	eng := machine.NewEngine(reg)
	eng.RegisterInterpreter(func(c context.Context, store *machine.Store, cmd machine.Command) ([]machine.Event, error) {
		switch cmd.(type) {
		case SendProviderAction:
			return handleSendProvider(ctx, store, runner)
		case ResolveCodecsAction:
			return runRunnerInterpret(c, store, cmd, runner)
		case EncodeProviderRequestAction:
			return runRunnerInterpret(c, store, cmd, runner)
		case ResolveProviderIngressAction:
			return runRunnerInterpret(c, store, cmd, runner)
		case DecodeProviderEnvelopeAction:
			return runRunnerInterpret(c, store, cmd, runner)
		case EncodeClientOutputAction:
			return runRunnerInterpret(c, store, cmd, runner)
		case recordEvidence:
			return handleRecordEvidence(c, store, sink, endpoint)
		default:
			return nil, nil
		}
	})

	if err := validateReplayRuntime(runner); err != nil {
		return RequestOutput{}, err
	}
	replayInfo, err := allocateReplayState(ctx, exchangeID, runner.ResponseIDs)
	if err != nil {
		return RequestOutput{}, err
	}

	store := machine.NewStore(
		machine.StateCell{Value: reflect.ValueOf(exchangeState{
			ExchangeID:     exchangeID,
			ClientHandler:  clientHandler,
			ClientFamily:   clientFamily,
			ClientDelivery: clientDelivery,
			Request:        request,
			Endpoint:       endpoint,
			Timing:         timing,
		})},
		machine.StateCell{Value: reflect.ValueOf(outcomeState{})},
		machine.StateCell{Value: reflect.ValueOf(attemptState{})},
		// Seed empty runner state cells so the store can assemble composite
		// states that reference them.
		machine.StateCell{Value: reflect.ValueOf(ExchangeInput{})},
		machine.StateCell{Value: reflect.ValueOf(replayInfo)},
		machine.StateCell{Value: reflect.ValueOf(CodecsResolvedEvent{})},
		machine.StateCell{Value: reflect.ValueOf(EncodedRequestState{})},
		machine.StateCell{Value: reflect.ValueOf(ProviderResponseState{})},
		machine.StateCell{Value: reflect.ValueOf(DecodedEnvelopeState{})},
		machine.StateCell{Value: reflect.ValueOf(pipelineOutcomeState{})},
	)

	_, err = eng.Run(ctx, store, RoutePlannedEvent{})

	var outcome outcomeState
	_ = store.Get(&outcome)
	if err != nil && outcome.Err == nil {
		outcome.Err = canonical.InternalError(err.Error())
	}
	return RequestOutput{
		Response:           outcome.Response,
		Target:             outcome.Target,
		CommitTrafficEvent: outcome.CommitTrafficEvent,
	}, outcome.Err
}

// ---- machine state (routing layer) ----

type exchangeState struct {
	ExchangeID     string
	ClientHandler  trafficevidence.ClientHandler
	ClientFamily   canonical.ClientFamily
	ClientDelivery delivery.Delivery
	Request        canonical.CanonicalRequest
	Endpoint       endpointintent.Endpoint
	Timing         *trafficevidence.Timing
}

type routeState struct {
	Plan []routing.Attempt
}

type attemptState struct {
	Index   int
	Current routing.Attempt
}

type outcomeState struct {
	Response           TransportResponse
	Target             RoutableTarget
	CommitTrafficEvent func(context.Context, error) error
	Err                error
	Terminal           bool
}

// ---- events ----

type RoutePlannedEvent struct{}
type AttemptSelectedEvent struct{}
type providerSendRequested struct{}
type providerFailed struct {
	Class routing.FailureClass
	Err   error
}
type ExchangeTerminalEvent struct{}
type planExhaustedState struct{}
type AttemptRetryScheduledEvent struct {
	NextIdx int
}

// ---- commands ----

type SendProviderAction struct{}
type recordEvidence struct{}

// ---- routing reducers ----

func planRoute(s exchangeState, e RoutePlannedEvent) (routeState, []machine.Event, []machine.Command, error) {
	wr := endpointToWorkspaceRouting(s.Endpoint)
	trace := &routing.Trace{ExchangeID: s.ExchangeID, Workspace: wr.WorkspaceSlug}
	plan := routing.BuildPlan(s.ExchangeID, wr.WorkspaceSlug, s.Request.Model(), routeTargets(wr), trace)
	return routeState{Plan: plan}, []machine.Event{machine.Event(AttemptSelectedEvent{})}, nil, nil
}

func selectNextAttempt(s struct {
	Route    routeState
	Attempt  attemptState
	Exchange exchangeState
}, e AttemptSelectedEvent) (attemptState, []machine.Event, []machine.Command, error) {
	if len(s.Route.Plan) == 0 {
		return attemptState{}, []machine.Event{machine.Event(ExchangeTerminalEvent{})}, nil, errors.New("no viable attempt (empty plan)")
	}
	return attemptState{
		Index:   0,
		Current: s.Route.Plan[0],
	}, []machine.Event{machine.Event(providerSendRequested{})}, nil, nil
}

func handleProviderFailed(s struct {
	Outcome outcomeState
	Route   routeState
	Attempt attemptState
}, e providerFailed) (outcomeState, []machine.Event, []machine.Command, error) {
	nextIdx := s.Attempt.Index + 1
	s.Outcome.Err = e.Err
	if nextIdx >= len(s.Route.Plan) {
		s.Outcome.Terminal = true
		return s.Outcome, []machine.Event{
			machine.Event(planExhaustedState{}),
			machine.Event(ExchangeTerminalEvent{}),
		}, []machine.Command{machine.Command(recordEvidence{})}, nil
	}
	if !e.Class.IsRetryable() {
		s.Outcome.Terminal = true
		return s.Outcome, []machine.Event{machine.Event(ExchangeTerminalEvent{})}, []machine.Command{machine.Command(recordEvidence{})}, nil
	}
	return s.Outcome, []machine.Event{machine.Event(AttemptRetryScheduledEvent{NextIdx: nextIdx})}, nil, nil
}

func scheduleRetry(s struct {
	Route   routeState
	Attempt attemptState
}, e AttemptRetryScheduledEvent) (attemptState, []machine.Event, []machine.Command, error) {
	if e.NextIdx >= len(s.Route.Plan) || len(s.Route.Plan) == 0 {
		return attemptState{}, []machine.Event{machine.Event(ExchangeTerminalEvent{})}, nil, errors.New("no viable attempt")
	}
	return attemptState{
		Index:   e.NextIdx,
		Current: s.Route.Plan[e.NextIdx],
	}, []machine.Event{machine.Event(providerSendRequested{})}, nil, nil
}

// requestProviderSend transitions from routing to the runner pipeline.
// It emits the sendProvider command; the seeded interpreter builds and
// stores the ExchangeInput, then emits pipelineStarted to begin the codec
// pipeline.
func requestProviderSend(_ outcomeState, _ providerSendRequested) (outcomeState, []machine.Event, []machine.Command, error) {
	return outcomeState{}, nil, []machine.Command{machine.Command(SendProviderAction{})}, nil
}

// terminalSuccess bridges the runner pipeline back to routing.
// It listens for pipelineCompleted and translates success into exchangeTerminal
// or failure into providerFailed.
func terminalSuccess(s struct {
	Pipeline pipelineOutcomeState
	Outcome  outcomeState
}, _ PipelineCompletedEvent) (outcomeState, []machine.Event, []machine.Command, error) {
	if s.Pipeline.Err != nil {
		class := mapErrorToFailureClass(s.Pipeline.Err)
		return outcomeState{Err: s.Pipeline.Err, Target: s.Outcome.Target},
			[]machine.Event{machine.Event(providerFailed{Class: class, Err: s.Pipeline.Err})},
			nil, nil
	}
	return outcomeState{Response: s.Pipeline.Response, Target: s.Outcome.Target, Err: nil},
		[]machine.Event{machine.Event(ExchangeTerminalEvent{})},
		[]machine.Command{machine.Command(recordEvidence{})},
		nil
}

// ---- command interpreters (unified) ----

// handleSendProvider seeds the runner pipeline: it reads routing state,
// builds the ExchangeInput and outcome.Target, and emits pipelineStarted.
// On path-build failure it emits providerFailed directly.
// It also prepares replay materialization when the selected target requires it.
func handleSendProvider(ctx context.Context, store *machine.Store, runner Runner) ([]machine.Event, error) {
	var exchange exchangeState
	if err := store.Get(&exchange); err != nil {
		return nil, err
	}
	var attempt attemptState
	if err := store.Get(&attempt); err != nil {
		return nil, err
	}
	if err := validateReplayRuntime(runner); err != nil {
		var outcome outcomeState
		_ = store.Get(&outcome)
		outcome.Err = canonical.InternalError(err.Error())
		store.Put(reflect.TypeOf(outcome), reflect.ValueOf(outcome))
		return []machine.Event{machine.Event(providerFailed{Class: routing.FailureUnknown, Err: outcome.Err})}, nil
	}

	providerConfig := findProviderConfig(exchange.Endpoint, attempt.Current.Target.ID)
	if providerConfig.Ref().String() == "" {
		err := errors.New("provider config not found for target")
		var outcome outcomeState
		_ = store.Get(&outcome)
		outcome.Err = err
		store.Put(reflect.TypeOf(outcome), reflect.ValueOf(outcome))
		return []machine.Event{machine.Event(providerFailed{Class: routing.FailureUnknown, Err: err})}, nil
	}

	pathRecord, err := buildPathRecord(
		ctx,
		exchange.ExchangeID,
		exchange.Endpoint.Name(),
		providerConfig,
		exchange.ClientDelivery,
		exchange.Request,
	)
	if err != nil {
		var outcome outcomeState
		_ = store.Get(&outcome)
		outcome.Err = err
		store.Put(reflect.TypeOf(outcome), reflect.ValueOf(outcome))
		return []machine.Event{machine.Event(providerFailed{Class: routing.FailureUnknown, Err: err})}, nil
	}

	// Prepare replay: materialise native replay from store when present.
	request := pathRecord.Request
	var nativeReplay *replay.NativeRef
	var replayScope replay.Scope
	replayScope = unsafeLocalReplayScope(exchange.Endpoint.Name().String())
	targetKey := replayTargetKey(pathRecord.Target, pathRecord.ProtocolKind, pathRecord.Request.Model())
	preparedRequest, native, prepErr := replay.Prepare(ctx, runner.ReplayStore, replayScope, targetKey, pathRecord.Request)
	if prepErr != nil {
		var outcome outcomeState
		_ = store.Get(&outcome)
		outcome.Err = prepErr
		store.Put(reflect.TypeOf(outcome), reflect.ValueOf(outcome))
		return []machine.Event{machine.Event(providerFailed{Class: routing.FailureUnknown, Err: prepErr})}, nil
	}
	request = preparedRequest
	nativeReplay = native

	input := ExchangeInput{
		ExchangeID:       exchange.ExchangeID,
		ClientHandler:    exchange.ClientHandler,
		ClientFamily:     exchange.ClientFamily,
		ClientDelivery:   exchange.ClientDelivery,
		Timing:           exchange.Timing,
		Request:          request,
		ReplayScope:      replayScope,
		NativeReplay:     nativeReplay,
		Target:           pathRecord.Target,
		Contract:         NewExecutionContract(exchange.ClientDelivery).WithProviderDelivery(pathRecord.ProviderDelivery),
		ProviderProtocol: pathRecord.ProtocolKind,
		ProviderDelivery: pathRecord.ProviderDelivery,
	}
	store.Put(reflect.TypeOf(input), reflect.ValueOf(input))

	var outcome outcomeState
	_ = store.Get(&outcome)
	outcome.Target = pathRecord.Target
	store.Put(reflect.TypeOf(outcome), reflect.ValueOf(outcome))
	return []machine.Event{machine.Event(PipelineStartedEvent{})}, nil
}

// handleRecordEvidence persists terminal traffic evidence.
func handleRecordEvidence(
	c context.Context,
	store *machine.Store,
	sink TrafficEventSink,
	endpoint endpointintent.Endpoint,
) ([]machine.Event, error) {
	var exchange exchangeState
	if err := store.Get(&exchange); err != nil {
		return nil, err
	}
	var outcome outcomeState
	if err := store.Get(&outcome); err != nil {
		return nil, err
	}
	var attempt attemptState
	attemptCount := 1
	if err := store.Get(&attempt); err == nil {
		attemptCount = attempt.Index + 1
	}
	outcome.CommitTrafficEvent = func(ctx context.Context, writeErr error) error {
		if sink == nil {
			return nil
		}
		event, buildErr := buildTerminalTrafficEvent(
			endpoint,
			exchange.ExchangeID,
			exchange.ClientHandler,
			exchange.ClientFamily,
			exchange.Request,
			outcome.Target,
			outcome.Response,
			writeErr,
			attemptCount,
			exchange.Timing,
		)
		if buildErr != nil {
			return buildErr
		}
		sink.Append(ctx, event)
		return nil
	}
	store.Put(reflect.TypeOf(outcome), reflect.ValueOf(outcome))
	return nil, nil
}
