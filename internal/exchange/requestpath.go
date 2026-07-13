package exchange

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/machine"
	"github.com/swobuforge/swobu/internal/profile"
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
	clientFamily canonical.ClientFamily,
	clientDelivery delivery.Delivery,
	request canonical.CanonicalRequest,
	endpoint endpointintent.Endpoint,
) (TransportResponse, RoutableTarget, error) {
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
		case sendProvider:
			return handleSendProvider(ctx, store)
		case resolveCodecs:
			return runRunnerInterpret(c, store, cmd, runner)
		case encodeProviderRequest:
			return runRunnerInterpret(c, store, cmd, runner)
		case resolveProviderIngress:
			return runRunnerInterpret(c, store, cmd, runner)
		case decodeProviderEnvelope:
			return runRunnerInterpret(c, store, cmd, runner)
		case encodeClientOutputCmd:
			return runRunnerInterpret(c, store, cmd, runner)
		case recordEvidence:
			return handleRecordEvidence(c, store, sink, endpoint)
		default:
			return nil, nil
		}
	})

	store := machine.NewStore(
		machine.StateCell{Value: reflect.ValueOf(exchangeState{
			ExchangeID:     exchangeID,
			ClientFamily:   clientFamily,
			ClientDelivery: clientDelivery,
			Request:        request,
			Endpoint:       endpoint,
		})},
		machine.StateCell{Value: reflect.ValueOf(outcomeState{})},
		machine.StateCell{Value: reflect.ValueOf(attemptState{})},
		// Seed empty runner state cells so the store can assemble composite
		// states that reference them.
		machine.StateCell{Value: reflect.ValueOf(ExchangeInput{})},
		machine.StateCell{Value: reflect.ValueOf(codecsResolved{})},
		machine.StateCell{Value: reflect.ValueOf(encodedRequest{})},
		machine.StateCell{Value: reflect.ValueOf(providerResponse{})},
		machine.StateCell{Value: reflect.ValueOf(decodedEnvelope{})},
		machine.StateCell{Value: reflect.ValueOf(pipelineOutcome{})},
	)

	_, err := eng.Run(ctx, store, routePlanned{})

	var outcome outcomeState
	_ = store.Get(&outcome)
	if err != nil && outcome.Err == nil {
		outcome.Err = canonical.InternalError(err.Error())
	}
	return outcome.Response, outcome.Target, outcome.Err
}

// ---- machine state (routing layer) ----

type exchangeState struct {
	ExchangeID     string
	ClientFamily   canonical.ClientFamily
	ClientDelivery delivery.Delivery
	Request        canonical.CanonicalRequest
	Endpoint       endpointintent.Endpoint
}

type routeState struct {
	Plan []routing.Attempt
}

type attemptState struct {
	Index   int
	Current routing.Attempt
}

type outcomeState struct {
	Response TransportResponse
	Target   RoutableTarget
	Err      error
	Terminal bool
}

// ---- events ----

type routePlanned struct{}
type attemptSelected struct{}
type providerSendRequested struct{}
type providerFailed struct {
	Class routing.FailureClass
	Err   error
}
type exchangeTerminal struct{}
type planExhaustedState struct{}
type attemptRetryScheduled struct {
	NextIdx int
}

// ---- commands ----

type sendProvider struct{}
type recordEvidence struct{}

// ---- routing reducers ----

func planRoute(s exchangeState, e routePlanned) (routeState, []machine.Event, []machine.Command, error) {
	wr := endpointToWorkspaceRouting(s.Endpoint)
	trace := &routing.Trace{ExchangeID: s.ExchangeID, Workspace: wr.WorkspaceSlug}
	plan := routing.BuildPlan(s.ExchangeID, wr.WorkspaceSlug, s.Request.Model(), routeTargets(wr), trace)
	return routeState{Plan: plan}, []machine.Event{machine.Event(attemptSelected{})}, nil, nil
}

func selectNextAttempt(s struct {
	Route    routeState
	Attempt  attemptState
	Exchange exchangeState
}, e attemptSelected) (attemptState, []machine.Event, []machine.Command, error) {
	if len(s.Route.Plan) == 0 {
		return attemptState{}, []machine.Event{machine.Event(exchangeTerminal{})}, nil, errors.New("no viable attempt (empty plan)")
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
			machine.Event(exchangeTerminal{}),
		}, []machine.Command{machine.Command(recordEvidence{})}, nil
	}
	if !e.Class.IsRetryable() {
		s.Outcome.Terminal = true
		return s.Outcome, []machine.Event{machine.Event(exchangeTerminal{})}, []machine.Command{machine.Command(recordEvidence{})}, nil
	}
	return s.Outcome, []machine.Event{machine.Event(attemptRetryScheduled{NextIdx: nextIdx})}, nil, nil
}

func scheduleRetry(s struct {
	Route   routeState
	Attempt attemptState
}, e attemptRetryScheduled) (attemptState, []machine.Event, []machine.Command, error) {
	if e.NextIdx >= len(s.Route.Plan) || len(s.Route.Plan) == 0 {
		return attemptState{}, []machine.Event{machine.Event(exchangeTerminal{})}, nil, errors.New("no viable attempt")
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
	return outcomeState{}, nil, []machine.Command{machine.Command(sendProvider{})}, nil
}

// terminalSuccess bridges the runner pipeline back to routing.
// It listens for pipelineCompleted and translates success into exchangeTerminal
// or failure into providerFailed.
func terminalSuccess(s struct {
	Pipeline pipelineOutcome
	Outcome  outcomeState
}, _ pipelineCompleted) (outcomeState, []machine.Event, []machine.Command, error) {
	if s.Pipeline.Err != nil {
		class := mapErrorToFailureClass(s.Pipeline.Err)
		return outcomeState{Err: s.Pipeline.Err, Target: s.Outcome.Target},
			[]machine.Event{machine.Event(providerFailed{Class: class, Err: s.Pipeline.Err})},
			nil, nil
	}
	return outcomeState{Response: s.Pipeline.Response, Target: s.Outcome.Target, Err: nil},
		[]machine.Event{machine.Event(exchangeTerminal{})},
		[]machine.Command{machine.Command(recordEvidence{})},
		nil
}

// ---- command interpreters (unified) ----

// handleSendProvider seeds the runner pipeline: it reads routing state,
// builds the ExchangeInput and outcome.Target, and emits pipelineStarted.
// On path-build failure it emits providerFailed directly.
func handleSendProvider(ctx context.Context, store *machine.Store) ([]machine.Event, error) {
	var exchange exchangeState
	if err := store.Get(&exchange); err != nil {
		return nil, err
	}
	var attempt attemptState
	if err := store.Get(&attempt); err != nil {
		return nil, err
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

	input := ExchangeInput{
		ExchangeID:       exchange.ExchangeID,
		ClientFamily:     exchange.ClientFamily,
		ClientDelivery:   exchange.ClientDelivery,
		Request:          pathRecord.Request,
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
	return []machine.Event{machine.Event(pipelineStarted{})}, nil
}

// handleRecordEvidence persists terminal traffic evidence.
func handleRecordEvidence(
	c context.Context,
	store *machine.Store,
	sink TrafficEventSink,
	endpoint endpointintent.Endpoint,
) ([]machine.Event, error) {
	if sink == nil {
		return nil, nil
	}
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
	event, buildErr := buildTerminalTrafficEvent(
		endpoint,
		exchange.ExchangeID,
		exchange.ClientFamily,
		exchange.Request,
		outcome.Target,
		outcome.Response,
		outcome.Err,
		attemptCount,
	)
	if buildErr != nil {
		return nil, nil
	}
	sink.Append(c, event)
	return nil, nil
}

// ---- helpers ----

func mapErrorToFailureClass(err error) routing.FailureClass {
	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		status := backendErr.StatusCode
		switch {
		case status == 404:
			return routing.FailureNotFound
		case status == 400:
			return routing.FailureBadRequest
		case status == 401 || status == 403:
			return routing.FailureAuth
		case status == 429:
			return routing.FailureRateLimited
		case status >= 500 && status < 600:
			return routing.FailureServerError
		default:
			return routing.FailureNetwork
		}
	}
	return routing.FailureUnknown
}

func endpointToWorkspaceRouting(e endpointintent.Endpoint) routing.WorkspaceRouting {
	routes := map[string]routing.Route{}
	for _, pc := range e.ProviderConfigs() {
		modelID := pc.ModelID()
		if modelID == "" {
			continue
		}
		mod := routes[modelID]
		mod.ModelName = modelID
		mod.Targets = append(mod.Targets, providerConfigToTarget(pc))
		routes[modelID] = mod
	}

	var defaultModel string
	if sel := e.SelectedProviderConfig(); sel.Ref().String() != "" {
		defaultModel = sel.ModelID()
	}

	return routing.WorkspaceRouting{
		WorkspaceSlug: e.Name().String(),
		DefaultModel:  defaultModel,
		Routes:        routes,
	}
}

func providerConfigToTarget(pc endpointintent.ProviderConfig) routing.Target {
	return routing.Target{
		ID:            pc.Ref().String(),
		Provider:      pc.ProviderSpec().String(),
		CredentialRef: pc.CredentialRef(),
		Model:         pc.ModelID(),
		Protocol:      routing.ProtocolOverride(pc.ProviderProtocol()),
		Enabled:       true,
	}
}

func routeTargets(wr routing.WorkspaceRouting) []routing.Target {
	var all []routing.Target
	for _, r := range wr.Routes {
		all = append(all, r.Targets...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].ID < all[j].ID
	})
	return all
}

func findProviderConfig(endpoint endpointintent.Endpoint, targetID string) endpointintent.ProviderConfig {
	for _, pc := range endpoint.ProviderConfigs() {
		if pc.Ref().String() == targetID {
			return pc
		}
	}
	return endpointintent.ProviderConfig{}
}

func buildPathRecord(
	ctx context.Context,
	exchangeID string,
	endpointName endpointintent.EndpointName,
	providerConfig endpointintent.ProviderConfig,
	clientDelivery delivery.Delivery,
	request canonical.CanonicalRequest,
) (exchangePathRecord, error) {
	routeProfile, ok := profile.ResolveRouteProfile(
		providerConfig.ProviderSpec().String(),
		providerConfig.BaseURL(),
		providerConfig.CredentialRef(),
	)
	if !ok {
		return exchangePathRecord{}, canonical.BadEndpoint("selected provider route is unsupported")
	}
	_ = routeProfile

	target := toRoutableTarget(providerConfig)
	providerDelivery := clientDelivery

	protocolKind, err := resolveProviderProtocolKind(
		providerConfig.ProviderSpec().String(),
		providerConfig.ProviderProtocol(),
		providerConfig.ProtocolKind(),
		request,
	)
	if err != nil {
		return exchangePathRecord{}, err
	}
	target.ProtocolKind = protocolKind

	modelID := providerConfig.ModelID()
	if modelID == "" {
		return exchangePathRecord{}, canonical.BadRequest("selected provider model is not configured")
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      modelID,
		Items:      request.Items(),
		Tools:      request.Tools(),
		Turn:       request.Turn(),
		ToolPolicy: request.ToolPolicy(),
	})

	return exchangePathRecord{
		Request:          req,
		Target:           target,
		ProviderDelivery: providerDelivery,
		ProtocolKind:     protocolKind,
	}, nil
}

func toRoutableTarget(pc endpointintent.ProviderConfig) RoutableTarget {
	t := NewRoutableTarget(
		pc.Ref().String(),
		pc.ProviderSpec().String(),
		pc.BaseURL(),
		pc.CredentialRef(),
		pc.ProtocolKind(),
		"",
		pc.SelectedFrame(),
		pc.ProviderProtocol(),
	)
	t.AuthHeader = pc.AuthHeader()
	return t
}

func resolveProviderProtocolKind(
	targetSpec string,
	targetProtocol string,
	configured protocolkind.ProtocolKind,
	request canonical.CanonicalRequest,
) (protocolkind.ProtocolKind, error) {
	if !profile.SupportsSpec(targetSpec) {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	if strings.TrimSpace(request.Model()) == "" {
		return "", canonical.BadRequest("canonical request is required")
	}
	targetProtocol = strings.TrimSpace(targetProtocol)
	autoProtocol := profile.ProviderProtocolAuto
	if targetProtocol == "" || targetProtocol == autoProtocol {
		if configured != "" {
			return configured, nil
		}
		return "", canonical.BadEndpoint("provider protocol must be concrete")
	}
	protocol, _, ok := profile.ProviderProtocolKindAndFrame(targetSpec, targetProtocol)
	if ok {
		if configured != "" && protocol != configured {
			return "", canonical.BadEndpoint("selected provider protocol is inconsistent with configured protocol kind")
		}
		return protocol, nil
	}
	return "", canonical.BadEndpoint("selected provider protocol is unsupported")
}

// exchangePathRecord is the bridge carrier from routing decision to Runner.Run.
type exchangePathRecord struct {
	Request          canonical.CanonicalRequest
	Target           RoutableTarget
	ProviderDelivery delivery.Delivery
	ProtocolKind     protocolkind.ProtocolKind
}
