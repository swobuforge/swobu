package exchange

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/transform"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
	"github.com/swobuforge/swobu/internal/turnstate"
)

const (
	PublicModelIDSwobu = "swobu"
)

// RequestIngress runs one client request lifecycle at the exchange boundary.
type RequestIngress struct {
	endpoints    EndpointReader
	providerExec ProviderIngressResolver
	runner       Runner
	runtime      RuntimeResolver
	resolver     ExchangeRouteResolver
}

type RuntimePoliciesSpec struct {
	DeliverySelector DeliverySelector
	ObservationStore observation.Store
	ContinuationStore canonical.ContinuationStore
	TurnStateStore   turnstate.TurnStateStore
	EffectSink       effect.Sink
}

type RuntimeResolver interface {
	ClientCodec(canonical.ClientFamily) ClientCodec
	ProviderRequestDocumentEncoder(protocolkind.ProtocolKind) ProviderRequestDocumentEncoder
	ProviderEnvelopeDecoder(protocolkind.ProtocolKind, delivery.Delivery) ProviderEnvelopeDecoder
	ProviderDocumentDecoder(protocolkind.ProtocolKind, delivery.Delivery) ProviderDocumentDecoder
}

func NewIngress(endpoints EndpointReader, providerExec ProviderIngressResolver, runtime RuntimeResolver, policies RuntimePoliciesSpec) RequestIngress {
	selector := policies.DeliverySelector
	if selector == nil {
		selector = FixedDeliverySelector{}
	}
	continuation := canonical.NewContinuationRuntime(policies.ContinuationStore)
	sink := policies.EffectSink
	if sink == nil {
		if policies.ObservationStore != nil || policies.TurnStateStore != nil {
			sink = effect.StoreBackedSink{
				Observations: policies.ObservationStore,
				TurnState:    policies.TurnStateStore,
			}
		} else {
			sink = effect.NoopSink{}
		}
	}
	return RequestIngress{
		endpoints:    endpoints,
		providerExec: providerExec,
		runner:       Runner{ResolveProviderIngress: providerExec.ResolveProviderIngress, EffectSink: sink},
		runtime:      runtime,
		resolver: ExchangeRouteResolver{
			DeliverySelector: selector,
			Observations:     policies.ObservationStore,
			Continuation:     continuation,
		},
	}
}

type RequestInput struct {
	EndpointName    endpointintent.EndpointName
	Request         transportpkg.TransportRequest
	ClientFamily    canonical.ClientFamily
	ResponseFraming delivery.Framing
}

type RequestOutput struct {
	Response TransportResponse
	Target   RoutableTarget
}

// HandleRequest resolves the endpoint name, derives client semantics from the
// transport request, and runs one exchange lifecycle.
func (h RequestIngress) HandleRequest(ctx context.Context, in RequestInput) (RequestOutput, error) {
	if in.EndpointName.IsZero() {
		return RequestOutput{}, canonical.BadEndpoint("endpoint name is required")
	}
	if h.endpoints == nil {
		return RequestOutput{}, canonical.InternalError("endpoint reader is not configured")
	}
	endpoint, err := h.endpoints.GetEndpoint(ctx, in.EndpointName)
	if err != nil {
		return RequestOutput{}, canonical.BadEndpoint("endpoint could not be resolved")
	}
	return h.HandleRequestWithEndpoint(ctx, endpoint, in)
}

// HandleRequestWithEndpoint reuses the same request lifecycle when the caller
// already owns endpoint resolution truth, such as control-plane probes.
func (h RequestIngress) HandleRequestWithEndpoint(ctx context.Context, endpoint endpointintent.Endpoint, in RequestInput) (RequestOutput, error) {
	response, target, err := h.runExchangeResponse(ctx, endpoint, in)
	if err != nil {
		return RequestOutput{}, err
	}
	return RequestOutput{Response: response, Target: target}, nil
}

func (h RequestIngress) runExchangeResponse(ctx context.Context, endpoint endpointintent.Endpoint, in RequestInput) (TransportResponse, RoutableTarget, error) {
	if h.providerExec == nil {
		return TransportResponse{}, RoutableTarget{}, canonical.InternalError("provider ingress resolver is not configured")
	}
	if h.runtime == nil {
		return TransportResponse{}, RoutableTarget{}, canonical.InternalError("exchange runtime resolver is not configured")
	}

	normalizedPath, err := canonical.NormalizePath(in.Request.URL)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	if err := canonical.ValidateClientTransport(in.Request.Method, normalizedPath, false); err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	clientFamily := in.ClientFamily
	if clientFamily == "" {
		return TransportResponse{}, RoutableTarget{}, canonical.InternalError("client family is not configured")
	}
	clientCodec := h.runtime.ClientCodec(clientFamily)
	if clientCodec == nil {
		return TransportResponse{}, RoutableTarget{}, canonical.UnsupportedOperation("client family is not implemented")
	}
	// TODO unclear what is exchange id? is it request id, endpoint id, or something else?
	exchangeID := "exchange_" + strings.TrimSpace(in.EndpointName.String()) // swobu:io-string source=boundary

	requestDoc, err := newClientRequestDocument(clientFamily, in.Request)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	requestDoc, _, err = applyDocumentTransformStage(
		transform.Registry{},
		exchangeID,
		StageClientWireIn,
		requestDoc,
		delivery.BufferedDelivery(),
	)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	request, decodedDelivery, err := clientCodec.DecodeClientRequest(requestDoc)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	if strings.TrimSpace(request.Model()) == "" { // swobu:io-string source=domain
		return TransportResponse{}, RoutableTarget{}, canonical.BadRequest("canonical request is required")
	}
	clientDelivery := normalizeClientDelivery(decodedDelivery, in.ResponseFraming)
	resolvedRoute, err := h.resolver.Resolve(ctx, RouteResolutionInput{
		Endpoint:       endpoint,
		ClientDelivery: clientDelivery,
		Request:        request,
	})
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	contract := NewExecutionContract(clientDelivery).WithProviderDelivery(resolvedRoute.ProviderDelivery)
	response, err := h.runner.Run(ctx, ExchangeInput{
		ExchangeID:                     exchangeID,
		ClientFamily:                   clientFamily,
		ClientDelivery:                 contract.ClientDelivery,
		Request:                        resolvedRoute.Request,
		Target:                         resolvedRoute.Target,
		Contract:                       contract,
		ProviderProtocol:               resolvedRoute.ProtocolKind,
		ProviderDelivery:               contract.ProviderDelivery,
		ClientCodec:                    clientCodec,
		ProviderRequestDocumentEncoder: h.runtime.ProviderRequestDocumentEncoder(resolvedRoute.ProtocolKind),
		ProviderEnvelopeDecoder:        h.runtime.ProviderEnvelopeDecoder(resolvedRoute.ProtocolKind, contract.ProviderDelivery),
		ProviderDocumentDecoder:        h.runtime.ProviderDocumentDecoder(resolvedRoute.ProtocolKind, contract.ProviderDelivery),
	})
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	return response, resolvedRoute.Target, nil
}

func newClientRequestDocument(family canonical.ClientFamily, req transportpkg.TransportRequest) (carrier.WireDocument, error) {
	body, err := readTransportRequestBody(req.Body)
	if err != nil {
		return carrier.WireDocument{}, canonical.BadRequest("request body could not be read")
	}
	mediaType := strings.TrimSpace(req.Header.Get("Content-Type")) // swobu:io-string source=boundary
	if mediaType == "" {
		mediaType = "application/json"
	}
	return carrier.NewWireDocument(
		carrier.StageClientRequestIn,
		family,
		mediaType,
		cloneHeader(req.Header),
		body,
		carrier.Meta{},
	), nil
}

func readTransportRequestBody(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func NewTransportRequest(method string, url string, header http.Header, body []byte) transportpkg.TransportRequest {
	return transportpkg.TransportRequest{
		Method: method,
		URL:    url,
		Header: cloneHeader(header),
		Body:   io.NopCloser(bytes.NewReader(append([]byte(nil), body...))),
	}
}

func normalizeClientDelivery(decoded delivery.Delivery, framing delivery.Framing) delivery.Delivery {
	if decoded.Mode != delivery.Streaming || decoded.Framing != delivery.FramingNone || framing == delivery.FramingNone {
		return decoded
	}
	return delivery.StreamingDelivery(framing)
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

func (h RequestIngress) ListModels(ctx context.Context, in ListModelsInput) (ListModelsOutput, error) {
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
