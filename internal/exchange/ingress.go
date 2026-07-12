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
	endpoints EndpointReader
	graph     exchangeGraph
}

type RuntimePoliciesSpec struct {
	DeliverySelector  DeliverySelector
	ObservationStore  observation.Store
	ContinuationStore canonical.ContinuationStore
	TurnStateStore    turnstate.TurnStateStore
	EffectSink        effect.Sink
}

// RuntimeResolver provides client and provider codec lookup for request ingress and the exchange runner.
type RuntimeResolver interface {
	ClientCodec(canonical.ClientFamily) ClientCodec
	ProviderRequestDocumentEncoder(protocolkind.ProtocolKind) ProviderRequestDocumentEncoder
	ProviderEnvelopeDecoder(protocolkind.ProtocolKind, delivery.Delivery) ProviderEnvelopeDecoder
	ProviderDocumentDecoder(protocolkind.ProtocolKind, delivery.Delivery) ProviderDocumentDecoder
}

// ExecutionRuntime resolves client codecs and provider ingress for one exchange run.
type ExecutionRuntime interface {
	RuntimeResolver
	ProviderIngressResolver
}

func NewIngress(endpoints EndpointReader, runtime ExecutionRuntime, policies RuntimePoliciesSpec) RequestIngress {
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
		endpoints: endpoints,
		graph: exchangeGraph{
			DeliverySelector: selector,
			Observations:     policies.ObservationStore,
			Continuation:     continuation,
			Runner:           Runner{Runtime: runtime, Transforms: transform.Registry{}, EffectSink: sink},
		},
	}
}

type RequestInput struct {
	EndpointName    endpointintent.EndpointName
	Request         transportpkg.TransportRequest
	ClientFamily    canonical.ClientFamily
	ResponseFraming delivery.Framing
	// ExchangeID is the request-scoped identifier used for event and effect
	// tracing. Callers must supply one unique value per exchange run.
	ExchangeID string
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
	normalizedPath, err := canonical.NormalizePath(in.Request.URL)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	if err := canonical.ValidateClientTransport(in.Request.Method, normalizedPath, false); err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	if h.graph.Runner.Runtime == nil {
		return TransportResponse{}, RoutableTarget{}, canonical.InternalError("exchange runtime resolver is not configured")
	}
	clientFamily := in.ClientFamily
	if clientFamily == "" {
		return TransportResponse{}, RoutableTarget{}, canonical.InternalError("client family is not configured")
	}
	clientCodec := h.graph.Runner.Runtime.ClientCodec(clientFamily)
	if clientCodec == nil {
		return TransportResponse{}, RoutableTarget{}, canonical.UnsupportedOperation("client family is not implemented")
	}
	exchangeID := strings.TrimSpace(in.ExchangeID) // swobu:io-string source=boundary
	if exchangeID == "" {
		return TransportResponse{}, RoutableTarget{}, canonical.InternalError("exchange id is required")
	}

	requestDoc, err := newClientRequestDocument(clientFamily, in.Request)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	requestDocResult, err := applyDocumentMiddleware(
		ctx,
		transform.Registry{},
		exchangeID,
		"",
		"",
		"",
		transform.StageClientWireIn,
		clientRequestWireInPort(),
		requestDoc,
		delivery.BufferedDelivery(),
	)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	requestDoc = requestDocResult.Value
	request, decodedDelivery, err := clientCodec.DecodeClientRequest(requestDoc)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	if strings.TrimSpace(request.Model()) == "" { // swobu:io-string source=domain
		return TransportResponse{}, RoutableTarget{}, canonical.BadRequest("canonical request is required")
	}
	clientDelivery := normalizeClientDelivery(decodedDelivery, in.ResponseFraming)
	response, target, err := h.graph.Execute(ctx, exchangeGraphInput{
		ExchangeID:     exchangeID,
		ClientFamily:   clientFamily,
		ClientDelivery: clientDelivery,
		Request:        request,
		Endpoint:       endpoint,
	})
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	return response, target, nil
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
