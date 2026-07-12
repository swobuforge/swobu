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

type exchangeGraph struct {
	DeliverySelector DeliverySelector
	Observations     observation.Store
	Continuation     canonical.ContinuationRuntime
	Runner           Runner
}

type exchangeGraphInput struct {
	ExchangeID     string
	ClientFamily   canonical.ClientFamily
	ClientDelivery delivery.Delivery
	Request        canonical.CanonicalRequest
	Endpoint       endpointintent.Endpoint
}

type exchangePath struct {
	Request          canonical.CanonicalRequest
	Target           RoutableTarget
	ProviderDelivery delivery.Delivery
	ProtocolKind     protocolkind.ProtocolKind
}

func (g exchangeGraph) Execute(ctx context.Context, in exchangeGraphInput) (TransportResponse, RoutableTarget, error) {
	paths, err := g.buildPaths(ctx, in.ExchangeID, in.Endpoint, in.ClientDelivery, in.Request)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	if len(paths) == 0 {
		return TransportResponse{}, RoutableTarget{}, canonical.BadEndpoint("endpoint has no provider path")
	}
	return orderedFallbackExecutor{Runner: g.Runner}.Execute(ctx, in, paths)
}

func (g exchangeGraph) buildPaths(ctx context.Context, exchangeID string, endpoint endpointintent.Endpoint, clientDelivery delivery.Delivery, request canonical.CanonicalRequest) ([]exchangePath, error) {
	providerConfigs := orderedPathProviderConfigs(endpoint)
	if len(providerConfigs) == 0 {
		return nil, canonical.BadEndpoint("endpoint has no provider path")
	}
	paths := make([]exchangePath, 0, len(providerConfigs))
	var lastErr error
	for _, providerConfig := range providerConfigs {
		path, err := g.buildPath(ctx, exchangeID, endpoint.Name(), providerConfig, clientDelivery, request)
		if err != nil {
			if canAdvanceToNextPath(err) {
				lastErr = err
				continue
			}
			return nil, err
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, canonical.BadEndpoint("endpoint has no viable provider path")
	}
	return paths, nil
}

type exchangePathState struct {
	endpointName     endpointintent.EndpointName
	clientDelivery   delivery.Delivery
	request          canonical.CanonicalRequest
	target           RoutableTarget
	modelID          string
	providerDelivery delivery.Delivery
	protocolKind     protocolkind.ProtocolKind
}

func (g exchangeGraph) buildPath(ctx context.Context, exchangeID string, endpointName endpointintent.EndpointName, providerConfig endpointintent.ProviderConfig, clientDelivery delivery.Delivery, request canonical.CanonicalRequest) (exchangePath, error) {
	routeProfile, ok := profile.ResolveRouteProfile(
		providerConfig.ProviderSpec().String(),
		providerConfig.BaseURL(),
		providerConfig.CredentialRef(),
	)
	if !ok {
		return exchangePath{}, canonical.BadEndpoint("selected provider route is unsupported")
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
	target.AuthHeader = providerConfig.AuthHeader()
	modelID, err := providerConfigModelID(providerConfig)
	if err != nil {
		return exchangePath{}, err
	}
	builder := NewBuilder(NewPort[exchangePathState](PortID("exchange.path")))
	builder.Link(
		"path.materialize_request",
		func(_ context.Context, state exchangePathState) (Result[exchangePathState], error) {
			state.request = materializeRequestForExecution(state.request, state.modelID)
			return NewResult(state), nil
		},
	)
	builder.Link(
		"path.select_provider_delivery",
		func(ctx context.Context, state exchangePathState) (Result[exchangePathState], error) {
			providerDelivery, err := g.resolveProviderDelivery(ctx, state.clientDelivery, state.target)
			if err != nil {
				return Result[exchangePathState]{}, err
			}
			state.providerDelivery = providerDelivery
			return NewResult(state), nil
		},
		After[exchangePathState, exchangePathState]("path.materialize_request"),
	)
	builder.Link(
		"path.resolve_provider_protocol_kind",
		func(_ context.Context, state exchangePathState) (Result[exchangePathState], error) {
			protocolKind, err := resolveProviderProtocolKind(
				state.target.ProviderID(),
				state.target.ProviderProtocol,
				state.target.ProtocolKind,
				state.request,
			)
			if err != nil {
				return Result[exchangePathState]{}, err
			}
			state.protocolKind = protocolKind
			state.target.ProtocolKind = protocolKind
			return NewResult(state), nil
		},
		After[exchangePathState, exchangePathState]("path.select_provider_delivery"),
	)
	builder.Link(
		"path.prepare_request",
		func(ctx context.Context, state exchangePathState) (Result[exchangePathState], error) {
			preparedRequest, err := g.Continuation.PrepareRequest(
				ctx,
				canonical.NewContinuationNamespace(endpointName.String()),
				state.protocolKind,
				state.request,
			)
			if err != nil {
				return Result[exchangePathState]{}, err
			}
			state.request = preparedRequest
			return NewResult(state), nil
		},
		After[exchangePathState, exchangePathState]("path.resolve_provider_protocol_kind"),
	)
	step, err := builder.Build(Context{
		Go:         ctx,
		ExchangeID: exchangeID,
		Target:     &target,
		Delivery:   clientDelivery,
	}, func(_ context.Context, state exchangePathState) (Result[exchangePathState], error) {
		return NewResult(state), nil
	})
	if err != nil {
		return exchangePath{}, err
	}
	state, err := step(ctx, exchangePathState{
		endpointName:   endpointName,
		clientDelivery: clientDelivery,
		request:        request,
		target:         target,
		modelID:        modelID,
	})
	if err != nil {
		return exchangePath{}, err
	}
	return exchangePath{
		Request:          state.Value.request,
		Target:           state.Value.target,
		ProviderDelivery: state.Value.providerDelivery,
		ProtocolKind:     state.Value.protocolKind,
	}, nil
}

func orderedPathProviderConfigs(endpoint endpointintent.Endpoint) []endpointintent.ProviderConfig {
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
	// Preserve the explicitly selected provider first, then retain the
	// remaining declared order so graph execution stays deterministic.
	if selectedFound {
		ordered = append([]endpointintent.ProviderConfig{selectedConfig}, ordered...)
	}
	return ordered
}

func (g exchangeGraph) resolveProviderDelivery(ctx context.Context, clientDelivery delivery.Delivery, target RoutableTarget) (delivery.Delivery, error) {
	if target.SelectedFrame != "" {
		if _, ok := profile.StreamingForFrame(target.SelectedFrame); !ok {
			return delivery.BufferedDelivery(), canonical.BadEndpoint("selected provider frame is unsupported")
		}
	}
	var observed observation.Snapshot
	if g.Observations != nil {
		obs, queryErr := g.Observations.Query(ctx, observation.ObservationQuerySpec{
			RouteID:    target.BackendRef,
			ProviderID: target.ProviderID(),
			ModelID:    "",
		})
		if queryErr != nil {
			return delivery.BufferedDelivery(), canonical.InternalError("observation query failed")
		}
		observed = obs
	}
	selector := g.DeliverySelector
	if selector == nil {
		selector = FixedDeliverySelector{}
	}
	return selector.SelectProviderDelivery(ctx, clientDelivery, observed), nil
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
