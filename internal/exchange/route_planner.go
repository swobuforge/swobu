package exchange

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/profile"
)

// RoutePlanInput contains the endpoint and request facts needed to materialize
// one ordered route plan.
type RoutePlanInput struct {
	Endpoint       endpointintent.Endpoint
	ClientDelivery delivery.Delivery
	Request        canonical.CanonicalRequest
}

// RouteAttempt captures one ordered provider target candidate and the
// canonical request materialized for it.
type RouteAttempt struct {
	Request          canonical.CanonicalRequest
	Target           RoutableTarget
	ProviderDelivery delivery.Delivery
	ProtocolKind     protocolkind.ProtocolKind
}

// RoutePlan is the ordered fallback plan built from one endpoint.
type RoutePlan struct {
	Target           RoutableTarget
	ProviderDelivery delivery.Delivery
	ProtocolKind     protocolkind.ProtocolKind
	Request          canonical.CanonicalRequest
	Attempts         []RouteAttempt
}

// RoutePlanner materializes ordered route attempts for one endpoint.
type RoutePlanner struct {
	DeliverySelector DeliverySelector
	Observations     observation.Store
	Continuation     canonical.ContinuationRuntime
}

// Plan resolves the ordered route plan for one endpoint and request.
func (r RoutePlanner) Plan(ctx context.Context, in RoutePlanInput) (RoutePlan, error) {
	attempts, err := r.planAttempts(ctx, in.Endpoint, in.ClientDelivery, in.Request)
	if err != nil {
		return RoutePlan{}, err
	}
	selected := attempts[0]
	return RoutePlan{
		Target:           selected.Target,
		ProviderDelivery: selected.ProviderDelivery,
		ProtocolKind:     selected.ProtocolKind,
		Request:          selected.Request,
		Attempts:         attempts,
	}, nil
}

func (r RoutePlanner) planAttempts(ctx context.Context, endpoint endpointintent.Endpoint, clientDelivery delivery.Delivery, request canonical.CanonicalRequest) ([]RouteAttempt, error) {
	providerConfigs := orderedProviderConfigs(endpoint)
	attempts := make([]RouteAttempt, 0, len(providerConfigs))
	for _, providerConfig := range providerConfigs {
		attempt, err := r.planAttempt(ctx, endpoint.Name(), providerConfig, clientDelivery, request)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	return attempts, nil
}

type routeAttemptState struct {
	endpointName     endpointintent.EndpointName
	clientDelivery   delivery.Delivery
	request          canonical.CanonicalRequest
	target           RoutableTarget
	modelID          string
	providerDelivery delivery.Delivery
	protocolKind     protocolkind.ProtocolKind
}

// planAttempt uses the graph builder to keep attempt materialization order
// explicit: materialize request model, select delivery, resolve protocol kind,
// then prepare continuation-aware request truth.
func (r RoutePlanner) planAttempt(ctx context.Context, endpointName endpointintent.EndpointName, providerConfig endpointintent.ProviderConfig, clientDelivery delivery.Delivery, request canonical.CanonicalRequest) (RouteAttempt, error) {
	routeProfile, ok := profile.ResolveRouteProfile(
		providerConfig.ProviderSpec().String(),
		providerConfig.BaseURL(),
		providerConfig.CredentialRef(),
	)
	if !ok {
		return RouteAttempt{}, canonical.BadEndpoint("selected provider route is unsupported")
	}
	target := NewRoutableTarget(
		providerConfig.Ref().String(),
		providerConfig.ProviderSpec().String(),
		providerConfig.BaseURL(),
		providerConfig.CredentialRef(),
		providerConfig.ProtocolKind(),
		string(routeProfile.AuthKind),
		providerConfig.SelectedFrame(),
		providerConfig.ProviderProtocol(),
	)
	modelID, err := providerConfigModelID(providerConfig)
	if err != nil {
		return RouteAttempt{}, err
	}
	builder := NewBuilder(NewPort[routeAttemptState](PortID("route.attempt")))
	builder.Link(
		"route_attempt.materialize_request",
		func(_ context.Context, state routeAttemptState) (Result[routeAttemptState], error) {
			state.request = materializeRequestForExecution(state.request, state.modelID)
			return NewResult(state), nil
		},
	)
	builder.Link(
		"route_attempt.select_provider_delivery",
		func(ctx context.Context, state routeAttemptState) (Result[routeAttemptState], error) {
			providerDelivery, err := r.selectProviderDelivery(ctx, state.clientDelivery, state.target)
			if err != nil {
				return Result[routeAttemptState]{}, err
			}
			state.providerDelivery = providerDelivery
			return NewResult(state), nil
		},
		After[routeAttemptState, routeAttemptState]("route_attempt.materialize_request"),
	)
	builder.Link(
		"route_attempt.resolve_provider_protocol_kind",
		func(_ context.Context, state routeAttemptState) (Result[routeAttemptState], error) {
			protocolKind, err := resolveProviderProtocolKind(
				state.target.ProviderID(),
				state.target.ProviderProtocol,
				state.target.ProtocolKind,
				state.request,
			)
			if err != nil {
				return Result[routeAttemptState]{}, err
			}
			state.protocolKind = protocolKind
			state.target.ProtocolKind = protocolKind
			return NewResult(state), nil
		},
		After[routeAttemptState, routeAttemptState]("route_attempt.select_provider_delivery"),
	)
	builder.Link(
		"route_attempt.prepare_request",
		func(ctx context.Context, state routeAttemptState) (Result[routeAttemptState], error) {
			preparedRequest, err := r.Continuation.PrepareRequest(
				ctx,
				canonical.NewContinuationNamespace(endpointName.String()),
				state.protocolKind,
				state.request,
			)
			if err != nil {
				return Result[routeAttemptState]{}, err
			}
			state.request = preparedRequest
			return NewResult(state), nil
		},
		After[routeAttemptState, routeAttemptState]("route_attempt.resolve_provider_protocol_kind"),
	)
	step, err := builder.Build(Context{}, func(_ context.Context, state routeAttemptState) (Result[routeAttemptState], error) {
		return NewResult(state), nil
	})
	if err != nil {
		return RouteAttempt{}, err
	}
	state, err := step(ctx, routeAttemptState{
		endpointName:   endpointName,
		clientDelivery: clientDelivery,
		request:        request,
		target:         target,
		modelID:        modelID,
	})
	if err != nil {
		return RouteAttempt{}, err
	}
	return RouteAttempt{
		Request:          state.Value.request,
		Target:           state.Value.target,
		ProviderDelivery: state.Value.providerDelivery,
		ProtocolKind:     state.Value.protocolKind,
	}, nil
}

func orderedProviderConfigs(endpoint endpointintent.Endpoint) []endpointintent.ProviderConfig {
	providerConfigs := endpoint.ProviderConfigs()
	selected := endpoint.SelectedProviderConfigRef()
	ordered := make([]endpointintent.ProviderConfig, 0, len(providerConfigs))
	selectedConfig := endpointintent.ProviderConfig{}
	selectedFound := false
	for _, providerConfig := range providerConfigs {
		if providerConfig.Ref() == selected {
			selectedConfig = providerConfig
			selectedFound = true
			continue
		}
		ordered = append(ordered, providerConfig)
	}
	// Ordered fallback tries the selected provider first, then preserves the
	// remaining declared provider order for the next candidate attempts.
	if selectedFound {
		ordered = append([]endpointintent.ProviderConfig{selectedConfig}, ordered...)
	}
	return ordered
}

func (r RoutePlanner) selectProviderDelivery(ctx context.Context, clientDelivery delivery.Delivery, target RoutableTarget) (delivery.Delivery, error) {
	base := clientDelivery
	if target.SelectedFrame != "" {
		if _, ok := profile.StreamingForFrame(target.SelectedFrame); !ok {
			return delivery.BufferedDelivery(), canonical.BadEndpoint("selected provider frame is unsupported")
		}
	}
	route := RouteSpec{
		Client: ClientSurfaceSpec{
			Protocol: protocolIDFromKind(target.ProtocolKind),
			Delivery: DeliveryPolicy{Preferred: clientDelivery, Supported: []delivery.Delivery{clientDelivery}},
		},
		Provider: ProviderTargetSpec{
			Protocol: protocolIDFromKind(target.ProtocolKind),
			Model:    unknownModelProfile(),
			Delivery: DeliveryPolicy{Preferred: base, Supported: []delivery.Delivery{base, delivery.StreamingDelivery(delivery.FramingSSE), delivery.BufferedDelivery()}},
		},
	}
	if err := route.Validate(); err != nil {
		return delivery.BufferedDelivery(), canonical.BadEndpoint("route specification is invalid")
	}
	var observed observation.Snapshot
	if r.Observations != nil {
		obs, queryErr := r.Observations.Query(ctx, observation.ObservationQuerySpec{
			RouteID:    target.BackendRef,
			ProviderID: target.ProviderID(),
			ModelID:    "",
		})
		if queryErr != nil {
			return delivery.BufferedDelivery(), canonical.InternalError("observation query failed")
		}
		observed = obs
	}
	return r.DeliverySelector.SelectProviderDelivery(ctx, route, clientDelivery, observed), nil
}

func resolveProviderProtocolKind(targetSpec string, targetProtocol string, configured protocolkind.ProtocolKind, request canonical.CanonicalRequest) (protocolkind.ProtocolKind, error) {
	if !profile.SupportsSpec(targetSpec) {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	if strings.TrimSpace(request.Model()) == "" { // swobu:io-string source=domain
		return "", canonical.BadRequest("canonical request is required")
	}
	targetProtocol = strings.TrimSpace(targetProtocol) // swobu:io-string source=domain
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

func providerConfigModelID(providerConfig endpointintent.ProviderConfig) (string, error) {
	modelID := strings.TrimSpace(providerConfig.ModelID()) // swobu:io-string source=domain
	if modelID == "" {
		return "", canonical.BadRequest("selected provider model is not configured")
	}
	return modelID, nil
}

func materializeRequestForExecution(request canonical.CanonicalRequest, modelID string) canonical.CanonicalRequest {
	modelID = strings.TrimSpace(modelID) // swobu:io-string source=domain
	if modelID == "" {
		return request
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:       modelID,
		Items:       request.Items(),
		Tools:       request.Tools(),
		Turn:        request.Turn(),
		ToolPolicy:  request.ToolPolicy(),
		CacheIntent: request.CacheIntent(),
	})
}

func protocolIDFromKind(kind protocolkind.ProtocolKind) ProtocolID {
	switch kind {
	case protocolkind.ChatCompletions:
		return ProtocolOpenAIChatCompletions
	case protocolkind.Responses:
		return ProtocolOpenAIResponses
	case protocolkind.Completions:
		return ProtocolOpenAICompletions
	case protocolkind.Messages:
		return ProtocolAnthropicMessages
	default:
		return ""
	}
}
