package exchange

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/routetarget"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/profile"
)

type RouteResolutionInput struct {
	Endpoint       endpointintent.Endpoint
	ClientDelivery delivery.Delivery
	Request        canonical.CanonicalRequest
}

type RouteResolutionOutput struct {
	Target           RoutableTarget
	ProviderDelivery delivery.Delivery
	ProtocolKind     protocolkind.ProtocolKind
	Request          canonical.CanonicalRequest
}

type ExchangeRouteResolver struct {
	DeliverySelector DeliverySelector
	Observations     observation.Store
	Continuation     canonical.ContinuationRuntime
}

func (r ExchangeRouteResolver) Resolve(ctx context.Context, in RouteResolutionInput) (RouteResolutionOutput, error) {
	target, err := resolveRoutableTarget(in.Endpoint)
	if err != nil {
		return RouteResolutionOutput{}, err
	}
	modelID, err := selectedModelID(in.Endpoint)
	if err != nil {
		return RouteResolutionOutput{}, err
	}
	resolvedRequest := materializeRequestForExecution(in.Request, modelID)
	providerDelivery, err := r.selectProviderDelivery(ctx, in.ClientDelivery, target)
	if err != nil {
		return RouteResolutionOutput{}, err
	}
	protocolKind, err := resolveProviderProtocolKind(
		target.ProviderID(),
		target.ProviderProtocol,
		target.ProtocolKind,
		resolvedRequest,
	)
	if err != nil {
		return RouteResolutionOutput{}, err
	}
	target.ProtocolKind = protocolKind
	resolvedRequest, err = r.Continuation.PrepareRequest(
		ctx,
		canonical.NewContinuationNamespace(in.Endpoint.Name().String()),
		protocolKind,
		resolvedRequest,
	)
	if err != nil {
		return RouteResolutionOutput{}, err
	}
	return RouteResolutionOutput{
		Target:           target,
		ProviderDelivery: providerDelivery,
		ProtocolKind:     protocolKind,
		Request:          resolvedRequest,
	}, nil
}

func (r ExchangeRouteResolver) selectProviderDelivery(ctx context.Context, clientDelivery delivery.Delivery, target RoutableTarget) (delivery.Delivery, error) {
	base := clientDelivery
	if target.SelectedFrame != "" {
		if _, ok := profile.StreamingForFrame(target.SelectedFrame); !ok {
			return delivery.BufferedDelivery(), canonical.BadEndpoint("selected provider frame is unsupported")
		}
	}
	route := RouteSpec{
		Client: ClientSurfaceSpec{
			Protocol: protocolIDFromKind(target.ProtocolKind),
			Delivery: DeliveryPolicy{Preferred: clientDelivery, Supported: []Delivery{clientDelivery}},
		},
		Provider: ProviderTargetSpec{
			Protocol: protocolIDFromKind(target.ProtocolKind),
			Model:    unknownModelProfile(),
			Delivery: DeliveryPolicy{Preferred: base, Supported: []Delivery{base, delivery.StreamingDelivery(delivery.FramingSSE), delivery.BufferedDelivery()}},
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

func resolveRoutableTarget(endpoint endpointintent.Endpoint) (RoutableTarget, error) {
	resolved, err := routetarget.ResolveRoutableTarget(endpoint)
	if err != nil {
		return RoutableTarget{}, err
	}
	return NewRoutableTarget(
		resolved.ProviderConfig.Ref().String(),
		resolved.ProviderConfig.ProviderSpec().String(),
		resolved.ProviderConfig.BaseURL(),
		resolved.ProviderConfig.CredentialRef(),
		resolved.ProviderConfig.ProtocolKind(),
		string(resolved.RouteProfile.AuthKind),
		resolved.ProviderConfig.SelectedFrame(),
		resolved.ProviderConfig.ProviderProtocol(),
	), nil
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

func selectedModelID(endpoint endpointintent.Endpoint) (string, error) {
	modelID := strings.TrimSpace(endpoint.SelectedProviderConfig().ModelID()) // swobu:io-string source=domain
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
