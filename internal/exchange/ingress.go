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
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/effect"
	stage "github.com/swobuforge/swobu/internal/exchange/stage"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/routing"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

const (
	PublicModelIDSwobu = "swobu"
)

// RequestIngress runs one client request lifecycle at the exchange boundary.
type RequestIngress struct {
	workspaces      WorkspaceLookup
	runner          Runner
	trafficEvidence TrafficEventSink
}

type RuntimePoliciesSpec struct {
	ObservationStore observation.Store
	EffectSink       effect.Sink
	TrafficEventSink TrafficEventSink
	ReplayStore      replay.Store
	ResponseIDs      replay.ResponseIDGenerator
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

func NewIngress(workspaces WorkspaceLookup, runtime ExecutionRuntime, policies RuntimePoliciesSpec) RequestIngress {
	if policies.ReplayStore == nil {
		policies.ReplayStore = replay.NewMemoryStore()
	}
	if policies.ResponseIDs == nil {
		policies.ResponseIDs = replay.NewDefaultResponseIDGenerator()
	}
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
		workspaces:      workspaces,
		trafficEvidence: policies.TrafficEventSink,
		runner: Runner{
			Runtime:        runtime,
			StageMechanics: stage.StageMechanics{},
			EffectSink:     sink,
			ReplayStore:    policies.ReplayStore,
			ResponseIDs:    policies.ResponseIDs,
		},
	}
}

type RequestInput struct {
	Workspace       routing.WorkspaceSlug
	Request         transportpkg.TransportRequest
	ClientHandler   trafficevidence.ClientHandler
	ClientFamily    canonical.ClientFamily
	ResponseFraming delivery.Framing
	Timing          *trafficevidence.Timing
	// ExchangeID is the request-scoped identifier used for event and effect
	// tracing. Callers must supply one unique value per exchange run.
	ExchangeID string
}

type RequestOutput struct {
	Response           TransportResponse
	Target             RoutableTarget
	CommitTrafficEvent func(context.Context, error) error
}

// HandleRequest resolves the endpoint name, derives client semantics from the
// transport request, and runs one exchange lifecycle.
func (h RequestIngress) HandleRequest(ctx context.Context, in RequestInput) (RequestOutput, error) {
	if in.Workspace.String() == "" {
		return RequestOutput{}, canonical.BadEndpoint("workspace slug is required")
	}
	if h.workspaces == nil {
		return RequestOutput{}, canonical.InternalError("workspace lookup is not configured")
	}
	workspace, err := h.workspaces.GetWorkspace(ctx, in.Workspace)
	if err != nil {
		return RequestOutput{}, canonical.BadEndpoint("workspace could not be resolved")
	}
	return h.HandleRequestWithWorkspace(ctx, workspace, in)
}

// HandleRequestWithWorkspace reuses the request lifecycle when the caller
// already owns workspace resolution truth, such as control-plane probes.
func (h RequestIngress) HandleRequestWithWorkspace(ctx context.Context, workspace routing.Workspace, in RequestInput) (RequestOutput, error) {
	out, err := h.runExchangeResponse(ctx, workspace, in)
	if in.Timing == nil && out.CommitTrafficEvent != nil {
		_ = out.CommitTrafficEvent(ctx, err)
	}
	if err != nil {
		return RequestOutput{}, err
	}
	return out, nil
}

func (h RequestIngress) runExchangeResponse(ctx context.Context, workspace routing.Workspace, in RequestInput) (RequestOutput, error) {
	normalizedPath, err := canonical.NormalizePath(in.Request.URL)
	if err != nil {
		return RequestOutput{}, err
	}
	if err := canonical.ValidateClientTransport(in.Request.Method, normalizedPath, false); err != nil {
		return RequestOutput{}, err
	}
	if h.runner.Runtime == nil {
		return RequestOutput{}, canonical.InternalError("exchange runtime resolver is not configured")
	}
	clientFamily := in.ClientFamily
	if clientFamily == "" {
		return RequestOutput{}, canonical.InternalError("client family is not configured")
	}
	clientCodec := h.runner.Runtime.ClientCodec(clientFamily)
	if clientCodec == nil {
		return RequestOutput{}, canonical.UnsupportedOperation("client family is not implemented")
	}
	exchangeID := strings.TrimSpace(in.ExchangeID) // swobu:io-string source=boundary
	if exchangeID == "" {
		return RequestOutput{}, canonical.InternalError("exchange id is required")
	}

	requestDoc, err := newClientRequestDocument(clientFamily, in.Request)
	if err != nil {
		return RequestOutput{}, err
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
		return RequestOutput{}, err
	}
	requestDoc = requestDocResult.Value
	commitEffectsBestEffort(ctx, h.runner.EffectSink, exchangeID, requestDocResult.Effects)
	decodeResult, err := clientCodec.DecodeClientRequest(requestDoc)
	commitEffectsBestEffort(ctx, h.runner.EffectSink, exchangeID, decodeResult.Effects)
	if err != nil {
		return RequestOutput{}, err
	}
	request := decodeResult.Value.Request
	decodedDelivery := decodeResult.Value.Delivery
	if strings.TrimSpace(request.Model()) == "" { // swobu:io-string source=domain
		return RequestOutput{}, canonical.BadRequest("canonical request is required")
	}
	clientDelivery := normalizeClientDelivery(decodedDelivery, in.ResponseFraming)
	out, err := runExchangeWithMachine(ctx, h.runner, h.trafficEvidence, exchangeID, in.ClientHandler, clientFamily, clientDelivery, request, workspace, in.Timing)
	if err != nil {
		return out, err
	}
	return out, nil
}

func buildTerminalTrafficEvent(workspace routing.Workspace, routeName routing.RouteName, exchangeID string, clientHandler trafficevidence.ClientHandler, clientFamily canonical.ClientFamily, request canonical.CanonicalRequest, target RoutableTarget, response TransportResponse, err error, attemptCount int, timing *trafficevidence.Timing) (trafficevidence.TrafficEvent, error) {
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
		RequestID:             requestID,
		Workspace:             workspace.Slug().String(),
		ClientHandler:         clientHandler,
		ClientFamily:          trafficevidence.ClientFamily(clientFamily),
		Route:                 route,
		Result:                result,
		StatusCode:            statusCode,
		Timing:                snapshotTiming(timing),
		AttemptCount:          max(attemptCount, 1),
		ModelRequested:        request.Model(),
		ModelResolved:         request.Model(),
		WorkspaceRouteModelID: routeName.String(),
		ExchangeDiagnostics:   requestOutcomeDiagnostics(err),
	}
	return trafficevidence.NewTerminalTrafficEvent(input)
}

func snapshotTiming(timing *trafficevidence.Timing) trafficevidence.Timing {
	if timing == nil {
		return trafficevidence.NewUnknownTiming()
	}
	return *timing
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

func newClientRequestDocument(family canonical.ClientFamily, req transportpkg.TransportRequest) (carrier.CarrierDocument, error) {
	body, err := readTransportRequestBody(req.Body)
	if err != nil {
		return carrier.CarrierDocument{}, canonical.BadRequest("request body could not be read")
	}
	mediaType := strings.TrimSpace(req.Header.Get("Content-Type")) // swobu:io-string source=boundary
	if mediaType == "" {
		mediaType = "application/json"
	}
	return carrier.NewCarrierDocument(
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
	Workspace routing.WorkspaceSlug
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
	if in.Workspace.String() == "" {
		return ListModelsOutput{}, canonical.BadEndpoint("workspace slug is required")
	}
	if h.workspaces == nil {
		return ListModelsOutput{}, canonical.InternalError("workspace lookup is not configured")
	}
	workspace, err := h.workspaces.GetWorkspace(ctx, in.Workspace)
	if err != nil {
		return ListModelsOutput{}, canonical.BadEndpoint("endpoint could not be resolved")
	}
	out := ListModelsOutput{
		DefaultModelID: workspace.DefaultRoute().String(),
		Models:         make([]ModelOption, 0, len(workspace.Routes())),
	}
	for _, route := range workspace.Routes() {
		option := ModelOption{
			ID:      route.Name().String(),
			ModelID: route.Name().String(),
		}
		if tiers := route.Tiers(); len(tiers) > 0 && len(tiers[0].Targets()) > 0 {
			option.ProviderSpec = string(tiers[0].Targets()[0].Provider())
			option.BackendRef = tiers[0].Targets()[0].ID().String()
		}
		out.Models = append(out.Models, option)
	}
	return out, nil
}
