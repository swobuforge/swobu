package exchange

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/productprojection"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
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
	ObservationStore  observation.Store
	ExecutionEvidence observation.ExecutionEvidenceSink
	CheckpointStore   continuity.Store
	ResponseIDs       ResponseIDGenerator
	ImageFetcher      provider.ImageFetcher
	PolicyResolver    WorkspacePolicyResolver
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
}

func NewIngress(workspaces WorkspaceLookup, runtime ExecutionRuntime, policies RuntimePoliciesSpec) RequestIngress {
	if policies.CheckpointStore == nil {
		policies.CheckpointStore = continuity.NewMemoryStore()
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
			Runtime:           runtime,
			ExecutionEvidence: policies.ExecutionEvidence,
			CheckpointStore:   policies.CheckpointStore,
			ResponseIDs:       policies.ResponseIDs,
			ImageFetcher:      policies.ImageFetcher,
			PolicyResolver:    policyResolver,
			TargetBackoff:     newTargetBackoffLedger(),
			TargetExceptions:  newTargetExceptions(),
		},
	}
}

type RequestInput struct {
	AccessID          accessidentity.ID
	Workspace         routing.WorkspaceSlug
	Request           carrier.TransportRequest
	ClientFamily      clientfamily.Family
	Operation         canonical.ClientOperation
	ResponseFraming   delivery.Framing
	Timing            *observation.Timing
	ExecutionAffinity executionaffinity.Key
	// ClientRequestID is protocol correlation only. RequestIngress owns
	// canonical ExchangeID allocation after admission reaches this boundary.
	ClientRequestID string
}

type RequestOutput struct {
	Response                ClientResponse
	Target                  provider.TargetSnapshot
	ProductObservationInput *ProductObservationInputData
	Compatibility           *wire.ResponseCompletion
	// AttemptCount is the authoritative number of issued provider commands.
	// It lets the terminal HTTP log prove a rejected request never contacted a target.
	AttemptCount int
	// FinalizeExecutionEvidence is owned by the inbound delivery boundary.
	// Exchange execution is semantically complete before this function runs;
	// the delivery owner supplies the actual terminal result exactly once.
	FinalizeExecutionEvidence func(delivery.Result)
}

// ProductObservationInputData carries only the bounded telemetry dimensions
// established before the inbound delivery result and final timing are known.
type ProductObservationInputData struct {
	clientProduct       clientfamily.Family
	requestedModel      string
	resolvedModel       string
	clientProtocol      canonical.ClientFamily
	requestPath         canonical.NormalizedPath
	provider            string
	targetProtocol      string
	responseStatus      int
	attemptCount        int
	fallbackRecovered   bool
	continuityRecovered bool
	wireMutated         bool
	crossProtocol       bool
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
		if errors.Is(err, routing.ErrNotFound) {
			return RequestOutput{}, canonical.BadEndpoint(workspaceNotFoundMessage(in.Workspace))
		}
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
	if !in.AccessID.Valid() {
		return RequestOutput{}, canonical.InternalError("access ID is required")
	}
	operation := in.Operation
	if operation.Family == "" {
		return RequestOutput{}, canonical.InternalError("client operation is not configured")
	}
	normalizedPath := operation.NormalizedPath
	if h.runner.Runtime == nil {
		return RequestOutput{}, canonical.InternalError("exchange runtime resolver is not configured")
	}
	clientFamily := operation.Family
	if clientFamily == "" {
		return RequestOutput{}, canonical.InternalError("client family is not configured")
	}
	clientCodec := h.runner.Runtime.ClientCodec(clientFamily)
	if clientCodec == nil {
		return RequestOutput{}, canonical.NotImplemented("Swobu has no client codec for this request family")
	}
	clientRequestID := strings.TrimSpace(in.ClientRequestID) // swobu:io-string source=boundary
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

	requestDoc, err := newClientRequestDocument(clientFamily, in.Request, runner.Policy.Limits.MaxRequestBytes, clientRequestID)
	if err != nil {
		return RequestOutput{}, err
	}
	decodeResult, err := clientCodec.DecodeClientRequest(requestDoc, operation)
	if err != nil {
		return RequestOutput{}, err
	}
	request := decodeResult.Request.Request
	decodedDelivery := decodeResult.Request.Delivery
	if strings.TrimSpace(request.Model()) == "" { // swobu:io-string source=domain
		return RequestOutput{}, canonical.BadRequest("canonical request is required")
	}
	canonicalExchangeID, identityErr := execution.NewExchangeID()
	if identityErr != nil {
		return RequestOutput{}, canonical.InternalError("exchange identity could not be allocated")
	}
	clientDelivery := normalizeClientDelivery(decodedDelivery, in.ResponseFraming)
	// Exchange admission is complete only after transport decoding, canonical
	// request validation, and delivery normalization. Failures before this
	// point must not fabricate an admitted exchange lifecycle.
	if runner.ExecutionEvidence != nil {
		started, startErr := executionevidence.NewExchangeStarted(canonicalExchangeID, in.AccessID, workspace.Slug(), time.Now(), in.ClientFamily, clientFamily, normalizedPath)
		if startErr != nil {
			return RequestOutput{}, canonical.InternalError("exchange evidence could not be started")
		}
		if err := runner.ExecutionEvidence.AppendExecutionEvidence(started); err != nil {
			slog.Warn("execution evidence append failed", "exchange_id", canonicalExchangeID.String(), "error", err)
		}
	}
	// The normalized path is threaded into the exchange input so terminal evidence
	// is complete on both success and failure — an exchange error finalizes the
	// evidence inside runExchange, where it already holds the path.
	out, err := runExchange(ctx, runner, in.AccessID, clientRequestID, canonicalExchangeID, in.ClientFamily, clientFamily, clientDelivery, decodeResult.Request, decodeResult.Changes, workspace, in.Timing, normalizedPath, in.ExecutionAffinity)
	if err != nil {
		return out, err
	}
	return out, nil
}

func BuildProductObservation(evidence *ProductObservationInputData, result delivery.Result) *productprojection.Observation {
	if evidence == nil {
		return nil
	}
	status := productObservationStatus(result, evidence.responseStatus)
	return &productprojection.Observation{
		ClientFamily:   string(evidence.clientProduct),
		RequestedModel: evidence.requestedModel, ResolvedModel: evidence.resolvedModel,
		ClientProtocol: string(evidence.clientProtocol), Operation: string(evidence.requestPath),
		Provider: evidence.provider, TargetProtocol: evidence.targetProtocol,
		Result: string(terminalProductResult(result)), StatusCode: status, DeliveryKind: string(result.Kind),
		ErrorCode: string(terminalCanonicalErrorCode(result.Err)), AttemptCount: evidence.attemptCount,
		FallbackRecovered: evidence.fallbackRecovered, ContinuityRecovered: evidence.continuityRecovered,
		WireMutated:   evidence.wireMutated,
		CrossProtocol: evidence.crossProtocol,
	}
}

// productObservationStatus preserves the public delivery status semantics in
// the deliberately lossy telemetry projection without exposing provider prose.
func productObservationStatus(result delivery.Result, responseStatus int) int {
	if result.Kind == delivery.ClientCancelled {
		return http.StatusRequestTimeout - 1 // 499, client closed request
	}
	if result.Kind == delivery.Succeeded && result.Err == nil {
		if result.HTTPStatus > 0 {
			return result.HTTPStatus
		}
		if responseStatus > 0 {
			return responseStatus
		}
		return http.StatusOK
	}
	var backendErr canonical.BackendError
	if errors.As(result.Err, &backendErr) && backendErr.StatusCode > 0 {
		return backendErr.StatusCode
	}
	var swobuErr canonical.Error
	if errors.As(result.Err, &swobuErr) {
		return requestOutcomeStatusForSwobuError(swobuErr.Code)
	}
	return http.StatusInternalServerError
}

func terminalProductResult(result delivery.Result) productprojection.ResultClass {
	if result.Kind == delivery.ClientCancelled {
		return productprojection.ResultCancelled
	}
	if result.Kind == delivery.Succeeded && result.Err == nil {
		return productprojection.ResultSuccess
	}
	var backendErr canonical.BackendError
	if errors.As(result.Err, &backendErr) {
		return productprojection.ResultBackendError
	}
	var swobuErr canonical.Error
	if errors.As(result.Err, &swobuErr) {
		return productprojection.ResultFromCanonical(string(swobuErr.Code))
	}
	return productprojection.ResultSwobuError
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
	case canonical.ErrorCodeNoAvailableTarget:
		return http.StatusServiceUnavailable
	case canonical.ErrorCodeProviderTimeout:
		return http.StatusGatewayTimeout
	case canonical.ErrorCodeProviderUnavailable:
		return http.StatusServiceUnavailable
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
	normalized := delivery.StreamingDelivery(framing)
	normalized.IncludeUsageFrame = decoded.IncludeUsageFrame
	return normalized
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
		if errors.Is(err, routing.ErrNotFound) {
			return ListModelsOutput{}, canonical.BadEndpoint(workspaceNotFoundMessage(in.Workspace))
		}
		return ListModelsOutput{}, canonical.BadEndpoint("endpoint could not be resolved")
	}
	return ListModelsWithWorkspace(workspace), nil
}

// ListModelsWithWorkspace projects the normal client-visible route surface
// from already-resolved routing truth.
func ListModelsWithWorkspace(workspace routing.Workspace) ListModelsOutput {
	out := ListModelsOutput{
		DefaultModelID: workspace.DefaultRoute().String(),
		Models:         make([]ModelOption, 0, len(workspace.Routes())),
	}
	for _, route := range workspace.Routes() {
		out.Models = append(out.Models, ModelOption{ID: route.Name().String()})
	}
	sort.Slice(out.Models, func(i, j int) bool { return out.Models[i].ID < out.Models[j].ID })
	return out
}

func workspaceNotFoundMessage(slug routing.WorkspaceSlug) string {
	return fmt.Sprintf("Workspace %q does not exist. Create it in Swobu or check the workspace name in this endpoint.", slug.String())
}
