package exchange

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/effect"
	stage "github.com/swobuforge/swobu/internal/exchange/stage"
	"github.com/swobuforge/swobu/internal/observation"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

const (
	PublicModelIDSwobu = "swobu"
)

// RequestIngress runs one client request lifecycle at the exchange boundary.
type RequestIngress struct {
	endpoints       EndpointReader
	runner          Runner
	trafficEvidence TrafficEventSink
}

type RuntimePoliciesSpec struct {
	ObservationStore  observation.Store
	EffectSink        effect.Sink
	TrafficEventSink  TrafficEventSink
	ContinuationStore canonical.ContinuationStore
}

// TrafficEventSink records immutable traffic events at the exchange
// boundary without creating a dependency back onto the broader ports layer.
type TrafficEventSink interface {
	Append(context.Context, trafficevidence.TrafficEvent)
}

// RuntimeResolver provides client codec lookup and provider protocol-bundle lookup for request ingress and the exchange runner.
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
	sink := policies.EffectSink
	if sink == nil {
		if policies.ObservationStore != nil {
			sink = effect.StoreBackedSink{
				Observations: policies.ObservationStore,
			}
		} else {
			sink = effect.NoopSink{}
		}
	}
	return RequestIngress{
		endpoints:       endpoints,
		trafficEvidence: policies.TrafficEventSink,
		runner: Runner{
			Runtime:           runtime,
			StageMechanics:    stage.StageMechanics{},
			EffectSink:        sink,
			ContinuationStore: policies.ContinuationStore,
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
	if h.runner.Runtime == nil {
		return TransportResponse{}, RoutableTarget{}, canonical.InternalError("exchange runtime resolver is not configured")
	}
	clientFamily := in.ClientFamily
	if clientFamily == "" {
		return TransportResponse{}, RoutableTarget{}, canonical.InternalError("client family is not configured")
	}
	clientCodec := h.runner.Runtime.ClientCodec(clientFamily)
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
	requestDocResult, err := applyDocumentPatches(
		ctx,
		stage.StageMechanics{},
		exchangeID,
		stage.StageClientWireIn,
		requestDoc,
		delivery.BufferedDelivery(),
	)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	requestDoc = requestDocResult.Value
	commitEffectsBestEffort(ctx, h.runner.EffectSink, exchangeID, requestDocResult.Effects)
	decodeResult, err := clientCodec.DecodeClientRequest(requestDoc)
	commitEffectsBestEffort(ctx, h.runner.EffectSink, exchangeID, decodeResult.Effects)
	if err != nil {
		return TransportResponse{}, RoutableTarget{}, err
	}
	request := decodeResult.Value.Request
	decodedDelivery := decodeResult.Value.Delivery
	if strings.TrimSpace(request.Model()) == "" { // swobu:io-string source=domain
		return TransportResponse{}, RoutableTarget{}, canonical.BadRequest("canonical request is required")
	}
	clientDelivery := normalizeClientDelivery(decodedDelivery, in.ResponseFraming)
	response, target, err := runExchangeWithMachine(ctx, h.runner, h.trafficEvidence, exchangeID, clientFamily, clientDelivery, request, endpoint)
	if err != nil {
		return response, target, err
	}
	return response, target, nil
}

func buildTerminalTrafficEvent(endpoint endpointintent.Endpoint, exchangeID string, clientFamily canonical.ClientFamily, request canonical.CanonicalRequest, target RoutableTarget, response TransportResponse, err error, attemptCount int) (trafficevidence.TrafficEvent, error) {
	requestID, parseErr := trafficevidence.ParseRequestID(strings.TrimSpace(exchangeID))
	if parseErr != nil {
		return trafficevidence.TrafficEvent{}, parseErr
	}
	if strings.TrimSpace(target.BackendRef) == "" { // swobu:io-string source=boundary
		return trafficevidence.TrafficEvent{}, fmt.Errorf("traffic evidence target is required")
	}
	route, routeErr := trafficevidence.NewRoute(target.BackendRef, request.Model())
	if routeErr != nil {
		return trafficevidence.TrafficEvent{}, routeErr
	}

	result, statusCode := requestOutcomeEvidence(err, response)
	input := trafficevidence.TrafficEventInput{
		RequestID:           requestID,
		Endpoint:            endpoint.Name().String(),
		ClientFamily:        trafficevidence.ClientFamily(clientFamily),
		Route:               route,
		Result:              result,
		StatusCode:          statusCode,
		AttemptCount:        max(attemptCount, 1),
		ModelRequested:      request.Model(),
		ModelResolved:       request.Model(),
		ExchangeDiagnostics: requestOutcomeDiagnostics(err),
	}
	return trafficevidence.NewTerminalTrafficEvent(input)
}

func requestOutcomeEvidence(err error, response TransportResponse) (trafficevidence.ResultClass, int) {
	if err == nil {
		statusCode := response.Transport.Status
		if statusCode <= 0 {
			statusCode = http.StatusOK
		}
		return trafficevidence.ResultClassSuccess, statusCode
	}

	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		statusCode := backendErr.StatusCode
		if statusCode <= 0 {
			statusCode = http.StatusBadGateway
		}
		return trafficevidence.ResultClassBackendError, statusCode
	}

	var swobuErr canonical.Error
	if errors.As(err, &swobuErr) {
		return requestOutcomeFromSwobuError(swobuErr.Code), requestOutcomeStatusForSwobuError(swobuErr.Code)
	}

	return trafficevidence.ResultClassSwobuError, http.StatusInternalServerError
}

func requestOutcomeFromSwobuError(code canonical.ErrorCode) trafficevidence.ResultClass {
	switch code {
	case canonical.ErrorCodeUnsupportedOperation:
		return trafficevidence.ResultClassUnsupportedOperation
	case canonical.ErrorCodeUnsupportedDelivery:
		return trafficevidence.ResultClassUnsupportedDeliveryVariant
	case canonical.ErrorCodeBadEndpoint, canonical.ErrorCodeUnsupportedEndpoint, canonical.ErrorCodeUnknownTarget:
		return trafficevidence.ResultClassSwobuError
	case canonical.ErrorCodeBadRequest, canonical.ErrorCodeInternal:
		return trafficevidence.ResultClassSwobuError
	default:
		return trafficevidence.ResultClassSwobuError
	}
}

func requestOutcomeStatusForSwobuError(code canonical.ErrorCode) int {
	switch code {
	case canonical.ErrorCodeBadRequest:
		return http.StatusBadRequest
	case canonical.ErrorCodeBadEndpoint, canonical.ErrorCodeUnsupportedEndpoint, canonical.ErrorCodeUnknownTarget:
		return http.StatusNotFound
	case canonical.ErrorCodeUnsupportedOperation, canonical.ErrorCodeUnsupportedDelivery:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func requestOutcomeDiagnostics(err error) []string {
	if err == nil {
		return nil
	}
	return []string{strings.TrimSpace(err.Error())} // swobu:io-string source=boundary
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
