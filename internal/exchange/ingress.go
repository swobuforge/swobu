package exchange

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/wire"
)

const (
	PublicModelIDSwobu = "swobu"
)

// RequestIngress runs one client request lifecycle at the exchange boundary.
type RequestIngress struct {
	workspaces WorkspaceLookup
	runner     runtimeBundle
}

type RuntimePoliciesSpec struct {
	ObservationStore observation.Store
	TrafficEvidence  observation.TrafficEventSink
	CheckpointStore  session.Store
	ResponseIDs      ResponseIDGenerator
	ImageFetcher     provider.ImageFetcher
	PolicyResolver   WorkspacePolicyResolver
}

// WorkspacePolicy is resolved once after workspace selection and then
// remains immutable for every provider attempt in the exchange.
type WorkspacePolicy struct {
	ImageFetch provider.ImageFetchPolicy
	Limits     RuntimeLimits
}

type WorkspacePolicyResolver interface {
	ResolveWorkspacePolicy(context.Context, routing.Workspace) (WorkspacePolicy, error)
}

type StaticWorkspacePolicyResolver struct{ Policy WorkspacePolicy }

func (r StaticWorkspacePolicyResolver) ResolveWorkspacePolicy(context.Context, routing.Workspace) (WorkspacePolicy, error) {
	return r.Policy.Clone(), nil
}

func DefaultWorkspacePolicy() WorkspacePolicy {
	return WorkspacePolicy{ImageFetch: provider.DefaultImageFetchPolicy(), Limits: DefaultRuntimeLimits()}
}

func (p WorkspacePolicy) Clone() WorkspacePolicy {
	p.ImageFetch = p.ImageFetch.Clone()
	return p
}

func (p WorkspacePolicy) Validate() error {
	if err := p.ImageFetch.Validate(); err != nil {
		return err
	}
	return p.Limits.Validate()
}

// RuntimeResolver provides client codec lookup for request ingress and the
// exchange runner. Exact provider codecs resolve through provider.BackendResolver.
type RuntimeResolver interface {
	ClientCodec(canonical.ClientFamily) ClientCodec
}

// ExecutionRuntime resolves client codecs and provider ingress for one exchange run.
type ExecutionRuntime interface {
	RuntimeResolver
	provider.BackendResolver
	provider.TargetSupportResolver
}

func NewIngress(workspaces WorkspaceLookup, runtime ExecutionRuntime, policies RuntimePoliciesSpec) RequestIngress {
	if policies.CheckpointStore == nil {
		policies.CheckpointStore = session.NewMemoryStore()
	}
	if policies.ResponseIDs == nil {
		policies.ResponseIDs = NewDefaultResponseIDGenerator()
	}
	policyResolver := policies.PolicyResolver
	if policyResolver == nil {
		policyResolver = StaticWorkspacePolicyResolver{Policy: DefaultWorkspacePolicy()}
	}
	return RequestIngress{
		workspaces: workspaces,
		runner: runtimeBundle{
			Runtime:         runtime,
			TrafficEvidence: policies.TrafficEvidence,
			CheckpointStore: policies.CheckpointStore,
			ResponseIDs:     policies.ResponseIDs,
			ImageFetcher:    policies.ImageFetcher,
			PolicyResolver:  policyResolver,
		},
	}
}

type RequestInput struct {
	Workspace       routing.WorkspaceSlug
	Request         carrier.TransportRequest
	ClientHandler   trafficevidence.ClientHandler
	ClientFamily    canonical.ClientFamily
	ResponseFraming delivery.Framing
	Timing          *trafficevidence.Timing
	// ExchangeID is the request-scoped identifier used for event and decision
	// tracing. Callers must supply one unique value per exchange run.
	ExchangeID string
}

type RequestOutput struct {
	Response        ClientResponse
	Target          provider.TargetSnapshot
	TrafficEvidence *TrafficEvidenceInput
	Compatibility   *wire.ResponseCompletion
}

// TrafficEvidenceInput is the immutable exchange fact set completed by the
// inbound delivery owner's concrete DeliveryResult.
type TrafficEvidenceInput struct {
	workspace     routing.Workspace
	routeName     routing.RouteName
	exchangeID    string
	clientHandler trafficevidence.ClientHandler
	clientFamily  canonical.ClientFamily
	requestPath   canonical.NormalizedPath
	request       canonical.CanonicalRequest
	target        provider.TargetSnapshot
	response      ClientResponse
	routing       terminalRoutingEvidence
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
	return h.runExchangeResponse(ctx, workspace, in)
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
		return RequestOutput{}, canonical.NotImplemented("Swobu has no client codec for this request family")
	}
	exchangeID := strings.TrimSpace(in.ExchangeID) // swobu:io-string source=boundary
	if exchangeID == "" {
		return RequestOutput{}, canonical.InternalError("exchange id is required")
	}

	runner := h.runner
	resolver := runner.PolicyResolver
	if resolver == nil {
		resolver = StaticWorkspacePolicyResolver{Policy: DefaultWorkspacePolicy()}
	}
	resolved, err := resolver.ResolveWorkspacePolicy(ctx, workspace)
	if err != nil {
		return RequestOutput{}, canonical.InternalError("workspace runtime policy could not be resolved")
	}
	if err := resolved.Validate(); err != nil {
		return RequestOutput{}, canonical.InternalError("workspace policy is invalid")
	}
	runner.Policy = resolved.Clone()

	requestDoc, err := newClientRequestDocument(clientFamily, in.Request, runner.Policy.Limits.MaxRequestBytes, exchangeID)
	if err != nil {
		return RequestOutput{}, err
	}
	decodeResult, err := clientCodec.DecodeClientRequest(requestDoc)
	if err != nil {
		return RequestOutput{}, err
	}
	request := decodeResult.Request.Request
	decodedDelivery := decodeResult.Request.Delivery
	if strings.TrimSpace(request.Model()) == "" { // swobu:io-string source=domain
		return RequestOutput{}, canonical.BadRequest("canonical request is required")
	}
	clientDelivery := normalizeClientDelivery(decodedDelivery, in.ResponseFraming)
	// The normalized path is threaded into the exchange input so terminal evidence
	// is complete on both success and failure — an exchange error finalizes the
	// evidence inside runExchange, where it already holds the path.
	out, err := runExchange(ctx, runner, exchangeID, in.ClientHandler, clientFamily, clientDelivery, decodeResult.Request, decodeResult.Changes, workspace, in.Timing, normalizedPath)
	if err != nil {
		return out, err
	}
	return out, nil
}

func BuildTerminalTrafficEvent(evidence *TrafficEvidenceInput, result delivery.Result, timing trafficevidence.Timing) (trafficevidence.TrafficEvent, error) {
	if evidence == nil {
		return trafficevidence.TrafficEvent{}, errors.New("traffic evidence input is absent")
	}
	requestID, parseErr := trafficevidence.ParseRequestID(strings.TrimSpace(evidence.exchangeID))
	if parseErr != nil {
		return trafficevidence.TrafficEvent{}, parseErr
	}
	if strings.TrimSpace(evidence.target.TargetID) == "" { // swobu:io-string source=boundary
		return trafficevidence.TrafficEvent{}, fmt.Errorf("traffic evidence target is required")
	}
	route, routeErr := trafficevidence.NewRoute(evidence.target.TargetID, evidence.request.Model())
	if routeErr != nil {
		return trafficevidence.TrafficEvent{}, routeErr
	}
	resultClass, statusCode := requestOutcomeEvidence(result, evidence.response)
	diagnostics := requestOutcomeDiagnostics(result.Err)
	if evidence.routing.possibleDuplicateExecution {
		diagnostics = append(diagnostics, "possible_duplicate_provider_execution_and_cost")
	}
	base := trafficevidence.TrafficEventInput{
		RequestID:             requestID,
		Workspace:             evidence.workspace.Slug().String(),
		ClientHandler:         evidence.clientHandler,
		ClientFamily:          trafficevidence.ClientFamily(evidence.clientFamily),
		RequestPath:           evidence.requestPath,
		Route:                 route,
		Timing:                timing,
		ModelRequested:        evidence.request.Model(),
		ModelResolved:         evidence.request.Model(),
		WorkspaceRouteModelID: evidence.routeName.String(),
		ProviderSpec:          profile.ProviderID(evidence.target.ProviderSpec),
		ProviderModel:         evidence.target.Model,
		ExchangeDiagnostics:   diagnostics,
	}
	outcome := trafficevidence.TerminalOutcome{
		Result:             resultClass,
		StatusCode:         statusCode,
		DeliveryKind:       result.Kind,
		CanonicalErrorCode: terminalCanonicalErrorCode(result.Err),
		AttemptCount:       evidence.routing.providerCallCount,
		FallbackRecovered:  evidence.routing.fallbackRecovered,
	}
	return trafficevidence.NewTerminalTrafficEvent(base, outcome)
}

func requestOutcomeEvidence(deliveryResult delivery.Result, response ClientResponse) (trafficevidence.ResultClass, int) {
	if deliveryResult.Kind == delivery.ClientCancelled {
		return trafficevidence.ResultClassCancelled, 499
	}
	err := deliveryResult.Err
	if deliveryResult.Kind == delivery.Succeeded && err == nil {
		statusCode := clientResponseStatus(response)
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

// terminalCanonicalErrorCode extracts the typed canonical error code from a
// terminal delivery error when the cause is already a canonical Swobu error. It
// returns "" for backend failures (which carry only an HTTP status, reported as
// status_code) and for deliveries without a typed canonical cause. This is a raw
// source fact carried on the product report; the analytical failure taxonomy is
// derived downstream. See product-telemetry.md.
func terminalCanonicalErrorCode(err error) canonical.ErrorCode {
	if err == nil {
		return ""
	}
	var swobuErr canonical.Error
	if errors.As(err, &swobuErr) {
		return swobuErr.Code
	}
	return ""
}

func clientResponseStatus(response ClientResponse) int {
	switch response := response.(type) {
	case BufferedResponse:
		return response.Response.Status
	case StreamingResponse:
		return response.Response.Status
	}
	return 0
}

func requestOutcomeFromSwobuError(code canonical.ErrorCode) trafficevidence.ResultClass {
	switch code {
	case canonical.ErrorCodeUnsupportedOperation:
		return trafficevidence.ResultClassUnsupportedOperation
	case canonical.ErrorCodeUnsupportedDelivery:
		return trafficevidence.ResultClassUnsupportedDeliveryVariant
	case canonical.ErrorCodeNotImplemented:
		return trafficevidence.ResultClassNotImplemented
	case canonical.ErrorCodeNoCompatibleTarget:
		return trafficevidence.ResultClassNoCompatibleTarget
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
	case canonical.ErrorCodeNotImplemented:
		return http.StatusNotImplemented
	case canonical.ErrorCodeNoCompatibleTarget:
		return http.StatusBadGateway
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

func newClientRequestDocument(family canonical.ClientFamily, req carrier.TransportRequest, maxBytes int64, exchangeID string) (carrier.Document, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultRuntimeLimits().MaxRequestBytes
	}
	if int64(len(req.Body)) > maxBytes {
		return carrier.Document{}, canonical.BadRequest("request body exceeds workspace limit")
	}
	mediaType := strings.TrimSpace(req.Header.Get("Content-Type")) // swobu:io-string source=boundary
	if mediaType == "" {
		mediaType = "application/json"
	}
	return carrier.NewDocument(
		family,
		mediaType,
		cloneHeader(req.Header),
		req.Body,
		carrier.Meta{Opaque: map[string]string{"exchange_id": strings.TrimSpace(exchangeID)}},
	), nil
}

func NewTransportRequest(method string, url string, header http.Header, body []byte) carrier.TransportRequest {
	return carrier.TransportRequest{
		Method: method,
		URL:    url,
		Header: cloneHeader(header),
		Body:   body,
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
	ID string
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
		out.Models = append(out.Models, ModelOption{ID: route.Name().String()})
	}
	return out, nil
}
