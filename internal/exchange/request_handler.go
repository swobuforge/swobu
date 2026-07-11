package exchange

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/routetarget"
	"github.com/swobuforge/swobu/internal/profile"
)

const (
	PublicModelIDSwobu     = "swobu"
	modelResolutionRuntime = "runtime"
	modelResolutionClient  = "client_swobu"
	modelResolutionIgnored = "client_ignored"
)

// RequestHandler runs one exchange lifecycle at the application boundary.
type RequestHandler struct {
	endpoints EndpointReader
	providers ProviderExecutor
	runner    Runner
	runtime   RuntimeResolver
}

type RuntimeResolver interface {
	ClientCodec(canonical.IngressFamily) ClientCodec
	ProviderRequestEncoder(protocolkind.ProtocolKind) ProviderRequestEncoder
	ProviderStreamDecoder(protocolkind.ProtocolKind, delivery.Delivery) ProviderStreamDecoder
	ProviderDocumentDecoder(protocolkind.ProtocolKind, delivery.Delivery) ProviderDocumentDecoder
}

func NewRequestHandler(endpoints EndpointReader, providers ProviderExecutor, runtime RuntimeResolver) RequestHandler {
	return RequestHandler{
		endpoints: endpoints,
		providers: providers,
		runner:    Runner{ProviderExecute: providers.Execute},
		runtime:   runtime,
	}
}

type HandleInput struct {
	EndpointName endpointintent.EndpointName
	ClientFamily canonical.IngressFamily
	Request      canonical.CanonicalRequest
	Contract     ExecutionContract
}

type HandleOutput struct {
	Response ProviderResponseStream
	Target   RoutableTarget
}

func (h RequestHandler) Handle(ctx context.Context, in HandleInput) (HandleOutput, error) {
	if in.EndpointName.IsZero() {
		return HandleOutput{}, canonical.BadEndpoint("endpoint name is required")
	}
	if strings.TrimSpace(in.Request.Model()) == "" { // swobu:io-string source=domain
		return HandleOutput{}, canonical.BadRequest("canonical request is required")
	}
	if in.ClientFamily == "" {
		return HandleOutput{}, canonical.BadRequest("client family is required")
	}
	if h.endpoints == nil {
		return HandleOutput{}, canonical.InternalError("endpoint reader is not configured")
	}
	endpoint, err := h.endpoints.GetEndpoint(ctx, in.EndpointName)
	if err != nil {
		return HandleOutput{}, canonical.BadEndpoint("endpoint could not be resolved")
	}
	return h.HandleWithEndpoint(ctx, endpoint, in)
}

func (h RequestHandler) HandleWithEndpoint(ctx context.Context, endpoint endpointintent.Endpoint, in HandleInput) (HandleOutput, error) {
	if h.providers == nil {
		return HandleOutput{}, canonical.InternalError("provider executor is not configured")
	}
	if h.runtime == nil {
		return HandleOutput{}, canonical.InternalError("exchange runtime resolver is not configured")
	}
	if strings.TrimSpace(in.Request.Model()) == "" { // swobu:io-string source=domain
		return HandleOutput{}, canonical.BadRequest("canonical request is required")
	}
	route, err := resolveRoute(endpoint)
	if err != nil {
		return HandleOutput{}, err
	}
	effectiveModel, err := selectedModelFromEndpoint(endpoint)
	if err != nil {
		return HandleOutput{}, err
	}
	contract := in.Contract
	providerDelivery, err := providerCallDeliveryPolicy(contract.ClientDelivery, route)
	if err != nil {
		return HandleOutput{}, err
	}
	contract = contract.WithProviderDelivery(providerDelivery)
	resolvedRequest := materializeRequestForExecution(in.Request, effectiveModel)
	protocolKind, err := resolveProviderProtocolForRequestWithVariant(
		route.ProviderID(),
		route.ProviderProtocol,
		route.ProtocolKind,
		resolvedRequest,
	)
	if err != nil {
		return HandleOutput{}, err
	}
	route.ProtocolKind = protocolKind
	runnerOut, err := h.runner.RunEnvelope(ctx, ClientInput{
		ExchangeID:       "exchange_" + strings.TrimSpace(in.EndpointName.String()), // swobu:io-string source=boundary
		ClientFamily:     in.ClientFamily,
		ClientDelivery:   contract.ClientDelivery,
		Request:          resolvedRequest,
		Target:           route,
		Contract:         contract,
		ProviderFamily:   route.ProtocolKind,
		ProviderDelivery: contract.ProviderDelivery,
		ClientCodec:      h.runtime.ClientCodec(in.ClientFamily),
		ProviderEncoder:  h.runtime.ProviderRequestEncoder(route.ProtocolKind),
		StreamDecoder:    h.runtime.ProviderStreamDecoder(route.ProtocolKind, contract.ProviderDelivery),
		DocumentDecoder:  h.runtime.ProviderDocumentDecoder(route.ProtocolKind, contract.ProviderDelivery),
	})
	if err != nil {
		return HandleOutput{}, err
	}
	if runnerOut.Envelope == nil {
		return HandleOutput{}, canonical.InternalError("exchange runner did not return provider envelope stream")
	}
	resp := NewEnvelopeStreamingProviderResponseStream(runnerOut.Envelope)
	return HandleOutput{
		Response: resp,
		Target:   route,
	}, nil
}

type ListModelsInput struct {
	EndpointName endpointintent.EndpointName
}

type ModelOption struct {
	ID           string
	ModelID      string
	ProviderSpec string
	BackendRef   string
}

type ListModelsOutput struct {
	DefaultModelID string
	Models         []ModelOption
}

func (h RequestHandler) ListModels(ctx context.Context, in ListModelsInput) (ListModelsOutput, error) {
	if in.EndpointName.IsZero() {
		return ListModelsOutput{}, canonical.BadEndpoint("endpoint name is required")
	}
	if h.endpoints == nil {
		return ListModelsOutput{}, canonical.InternalError("endpoint reader is not configured")
	}
	endpoint, err := h.endpoints.GetEndpoint(ctx, in.EndpointName)
	if err != nil {
		return ListModelsOutput{}, canonical.BadEndpoint("endpoint could not be resolved")
	}
	selected := endpoint.SelectedProviderConfig()
	return ListModelsOutput{
		DefaultModelID: PublicModelIDSwobu,
		Models: []ModelOption{{
			ID:           PublicModelIDSwobu,
			ModelID:      selected.ModelID(),
			ProviderSpec: selected.ProviderSpec().String(),
			BackendRef:   selected.Ref().String(),
		}},
	}, nil
}

func resolveRoute(endpoint endpointintent.Endpoint) (RoutableTarget, error) {
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

func materializeRequestForExecution(request canonical.CanonicalRequest, modelID string) canonical.CanonicalRequest {
	modelID = strings.TrimSpace(modelID) // swobu:io-string source=domain
	if modelID == "" {
		return request
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:              modelID,
		Items:              request.Items(),
		PreviousResponseID: request.PreviousResponseID(),
		ToolMode:           request.ToolMode(),
		CacheIntent:        request.CacheIntent(),
	})
}

func validateRequestedPublicModel(raw string) string {
	requested := strings.TrimSpace(raw) // swobu:io-string source=domain
	if requested == "" {
		return modelResolutionRuntime
	}
	if strings.EqualFold(requested, PublicModelIDSwobu) {
		return modelResolutionClient
	}
	return modelResolutionIgnored
}

func providerCallDeliveryPolicy(clientDelivery delivery.Delivery, target RoutableTarget) (delivery.Delivery, error) {
	providerDelivery := clientDelivery
	if target.SelectedFrame != "" {
		if _, ok := profile.StreamingForFrame(target.SelectedFrame); !ok {
			return delivery.BufferedDelivery(), canonical.BadEndpoint("selected provider frame is unsupported")
		}
	}
	return providerDelivery, nil
}

func resolveProviderProtocolForRequestWithVariant(providerSpec string, providerProtocol string, configured protocolkind.ProtocolKind, request canonical.CanonicalRequest) (protocolkind.ProtocolKind, error) {
	if !profile.SupportsSpec(providerSpec) {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	if strings.TrimSpace(request.Model()) == "" { // swobu:io-string source=domain
		return "", canonical.BadRequest("canonical request is required")
	}
	providerProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=domain
	if providerProtocol == "" || providerProtocol == profile.ProviderProtocolAuto {
		if configured != "" {
			return configured, nil
		}
		return "", canonical.BadEndpoint("provider protocol must be concrete")
	}
	if protocol, _, ok := profile.ProviderProtocolKindAndFrame(providerSpec, providerProtocol); ok {
		if configured != "" && protocol != configured {
			return "", canonical.BadEndpoint("selected provider protocol is inconsistent with configured protocol kind")
		}
		return protocol, nil
	}
	return "", canonical.BadEndpoint("selected provider protocol is unsupported")
}

func effectiveModelIDForRequest(selectedModelID string) (string, error) {
	selectedModelID = strings.TrimSpace(selectedModelID) // swobu:io-string source=domain
	if selectedModelID != "" {
		return selectedModelID, nil
	}
	return "", canonical.BadRequest("selected provider model is not configured")
}

func selectedModelFromEndpoint(endpoint endpointintent.Endpoint) (string, error) {
	return effectiveModelIDForRequest(endpoint.SelectedProviderConfig().ModelID())
}
