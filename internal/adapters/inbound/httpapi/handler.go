package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/protocolsurface"
	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/platform/httpcontent"
)

const (
	maxCompressedRequestBodyBytes int64 = 2 << 20
	maxDecodedRequestBodyBytes    int64 = 8 << 20
)

type RequestHandler interface {
	Handle(ctx context.Context, in requestpath.HandleInput) (requestpath.HandleOutput, error)
}

type ModelsHandler interface {
	ListModels(ctx context.Context, in requestpath.ListModelsInput) (requestpath.ListModelsOutput, error)
}

type Handler struct {
	requests RequestHandler
}

func NewHandler(requests RequestHandler) Handler {
	return Handler{requests: requests}
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	endpointName, operationPath, err := splitProtocolPath(r.URL.Path)
	if err != nil {
		writeSwobuError(w, canonical.UnsupportedEndpoint("unsupported endpoint URL"))
		return
	}
	if operationPath == "" {
		writeSwobuError(w, canonical.UnsupportedEndpoint("protocol operation path is required"))
		return
	}

	endpoint, err := endpointintent.ParseEndpointName(endpointName)
	if err != nil {
		writeSwobuError(w, canonical.BadEndpoint("endpoint name is invalid"))
		return
	}

	normalizedPath, err := canonical.NormalizePath(operationPath)
	if err != nil {
		writeCompatibilityError(w, err)
		return
	}
	if websocketUpgrade(r) {
		if normalizedPath == canonical.NormalizedPathResponses {
			h.serveResponsesWebsocket(w, r, endpointName, normalizedPath)
			return
		}
		writeCompatibilityError(w, canonical.UnsupportedEndpoint("websocket ingress is supported only on protocol /responses routes"))
		return
	}
	if normalizedPath == canonical.NormalizedPathModels {
		h.serveModelsEndpoint(w, r, endpoint)
		return
	}
	if err := canonical.ValidateIngressTransport(r.Method, normalizedPath, false); err != nil {
		writeCompatibilityError(w, err)
		return
	}

	family, err := canonical.InferFamily(r.Method, normalizedPath, strings.TrimSpace(r.Header.Get("anthropic-version")) != "") // swobu:io-string source=boundary
	if err != nil {
		writeCompatibilityError(w, err)
		return
	}

	requestBody, err := decodeRequestBody(w, r)
	if err != nil {
		writeCompatibilityError(w, err)
		return
	}

	request, streaming, err := decodeCanonicalRequest(family, requestBody)
	if err != nil {
		writeCompatibilityError(w, err)
		return
	}
	provenance := ingressProvenance(r, family, normalizedPath)
	requestID := requestIDFromRequest(r)
	logIngressRequestShape(requestID, endpoint.String(), provenance, request, streaming)

	if h.requests == nil {
		writeSwobuError(w, canonical.InternalError("request orchestrator is not configured"))
		return
	}

	out, err := h.requests.Handle(r.Context(), requestpath.HandleInput{
		EndpointName: endpoint,
		RequestID:    requestID,
		Request:      request,
		Contract:     requestpath.NewExecutionContract(streaming),
		Provenance:   provenance,
	})
	if err != nil {
		logRequestOutcome(requestID, endpoint.String(), provenance, "", "", "", "", "", "", err)
		writeCompatibilityError(w, err)
		return
	}
	defer func() {
		_ = out.Response.Close()
	}()
	writeModelResolutionHeaders(w, out.Response.Metadata())
	metadata := out.Response.Metadata()
	logRequestOutcome(
		requestID,
		endpoint.String(),
		provenance,
		metadata.ModelRequested,
		metadata.ModelResolved,
		metadata.ModelResolutionMode,
		metadata.ClientResponseMode,
		metadata.ProviderCallMode,
		metadata.ConversionKind,
		nil,
	)

	if err := writeSuccessResponse(w, requestID, family, out.Response, streaming); err != nil {
		writeCompatibilityError(w, err)
	}
}

func (h Handler) serveModelsEndpoint(w http.ResponseWriter, r *http.Request, endpoint endpointintent.EndpointName) {
	if r.Method != http.MethodGet {
		writeSwobuError(w, canonical.UnsupportedOperation("models endpoint only supports GET"))
		return
	}
	if h.requests == nil {
		writeSwobuError(w, canonical.InternalError("request orchestrator is not configured"))
		return
	}
	modelsHandler, ok := h.requests.(ModelsHandler)
	if !ok {
		writeSwobuError(w, canonical.InternalError("models query is not configured"))
		return
	}
	out, err := modelsHandler.ListModels(r.Context(), requestpath.ListModelsInput{EndpointName: endpoint})
	if err != nil {
		writeCompatibilityError(w, err)
		return
	}
	writeModelsSuccess(w, out)
}

// This path split is intentionally small and local to the HTTP edge.
// It parses endpoint-qualified protocol routes only; it is not a second
// routing layer and must not absorb family-specific semantics.
func splitProtocolPath(raw string) (string, string, error) {
	if !strings.HasPrefix(raw, "/c/") {
		return "", "", errors.New("missing /c/ prefix")
	}
	trimmed := strings.TrimPrefix(raw, "/c/")
	if trimmed == "" {
		return "", "", errors.New("missing endpoint name")
	}

	endpointName, suffix, found := strings.Cut(trimmed, "/")
	if endpointName == "" {
		return "", "", errors.New("missing endpoint name")
	}
	if !found {
		return endpointName, "", nil
	}
	suffix = "/" + strings.TrimLeft(suffix, "/")
	if suffix == "/" {
		return endpointName, "", nil
	}
	return endpointName, suffix, nil
}

func decodeRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	limitedBody := http.MaxBytesReader(w, r.Body, maxCompressedRequestBodyBytes)
	raw, err := io.ReadAll(limitedBody)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, canonical.BadRequest("request body exceeds maximum allowed size")
		}
		return nil, canonical.BadRequest("request body could not be read")
	}
	decoded, err := httpcontent.DecodeBytesLimited(r.Header.Get("Content-Encoding"), raw, maxDecodedRequestBodyBytes)
	if err != nil {
		return nil, canonical.BadRequest("request content encoding is unsupported, invalid, or exceeds size limits")
	}
	return decoded, nil
}

func decodeCanonicalRequest(family canonical.IngressFamily, raw []byte) (canonical.CanonicalRequest, bool, error) {
	codec, err := protocolsurface.ForIngressFamily(family)
	if err != nil {
		return nil, false, err
	}
	return codec.DecodeRequest(raw)
}

func logIngressRequestShape(
	requestID string,
	endpoint string,
	provenance requestpath.IngressProvenance,
	request canonical.CanonicalRequest,
	streaming bool,
) {
	threadCount, lastRole, hasPreviousResponseID := requestShapeSummary(request)
	slog.Debug("protocol ingress request",
		"component", "httpapi",
		"event", "ingress_request_shape",
		"request_id", requestID,
		"endpoint", endpoint,
		"ingress_family", string(provenance.IngressFamily),
		"normalized_op", string(provenance.NormalizedOp),
		"client_protocol", strings.TrimSpace(provenance.ClientProtocol), // swobu:io-string source=boundary
		"client_handler", strings.TrimSpace(provenance.ClientHandler), // swobu:io-string source=boundary
		"streaming", streaming,
		"item_count", threadCount,
		"last_input_role", lastRole,
		"has_previous_response_id", hasPreviousResponseID,
	)
}

func requestShapeSummary(request canonical.CanonicalRequest) (int, string, bool) {
	switch typed := request.(type) {
	case canonical.DialogCanonicalRequest:
		items := typed.Items()
		return len(items), lastRoleFromItems(items), false
	case canonical.GenerationCanonicalRequest:
		items := typed.Thread()
		return len(items), lastRoleFromItems(items), strings.TrimSpace(typed.PreviousResponseID()) != "" // swobu:io-string source=boundary
	case canonical.PromptCanonicalRequest:
		return 1, "user", false
	default:
		return 0, "", false
	}
}

func lastRoleFromItems(items []canonical.CanonicalItem) string {
	if len(items) == 0 {
		return ""
	}
	switch items[len(items)-1].Author {
	case canonical.ItemAuthorAssistant:
		return "assistant"
	case canonical.ItemAuthorTool:
		return "tool"
	default:
		return "user"
	}
}

func logRequestOutcome(
	requestID string,
	endpoint string,
	provenance requestpath.IngressProvenance,
	modelRequested string,
	modelResolved string,
	modelResolutionMode string,
	clientResponseMode string,
	providerCallMode string,
	conversionKind string,
	err error,
) {
	result := "success"
	statusCode := http.StatusOK
	errorOrigin := ""
	backendRef := ""
	if err != nil {
		result = "swobu_error"
		errorOrigin = string(canonical.ErrorOriginSwobu)
		var backendErr canonical.BackendError
		if errors.As(err, &backendErr) {
			result = "backend_error"
			statusCode = backendErr.StatusCode
			errorOrigin = string(canonical.ErrorOriginBackend)
			backendRef = strings.TrimSpace(backendErr.BackendRef) // swobu:io-string source=boundary
		} else {
			statusCode = statusCodeForCompatibilityError(err)
		}
	}
	slog.Debug("protocol request outcome",
		"component", "httpapi",
		"event", "request_outcome",
		"request_id", requestID,
		"endpoint", endpoint,
		"ingress_family", string(provenance.IngressFamily),
		"normalized_op", string(provenance.NormalizedOp),
		"client_protocol", strings.TrimSpace(provenance.ClientProtocol), // swobu:io-string source=boundary
		"client_handler", strings.TrimSpace(provenance.ClientHandler), // swobu:io-string source=boundary
		"result", result,
		"status_code", statusCode,
		"error_origin", errorOrigin,
		"backend_ref", backendRef,
		"model_requested", strings.TrimSpace(modelRequested), // swobu:io-string source=boundary
		"model_resolved", strings.TrimSpace(modelResolved), // swobu:io-string source=boundary
		"model_resolution_mode", strings.TrimSpace(modelResolutionMode), // swobu:io-string source=boundary
		"client_response_mode", strings.TrimSpace(clientResponseMode), // swobu:io-string source=boundary
		"provider_call_mode", strings.TrimSpace(providerCallMode), // swobu:io-string source=boundary
		"conversion_kind", strings.TrimSpace(conversionKind), // swobu:io-string source=boundary
	)
}

func statusCodeForCompatibilityError(err error) int {
	var swobuErr canonical.Error
	if errors.As(err, &swobuErr) {
		return statusCodeForSwobuError(swobuErr.Code)
	}
	return http.StatusInternalServerError
}

func writeCompatibilityError(w http.ResponseWriter, err error) {
	var swobuErr canonical.Error
	if errors.As(err, &swobuErr) {
		writeSwobuError(w, swobuErr)
		return
	}

	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		if backendErr.RetryAfterHeaderValue != "" {
			w.Header().Set("Retry-After", backendErr.RetryAfterHeaderValue)
		}
		if backendErr.Message != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(backendErr.StatusCode)
			_, _ = w.Write([]byte(backendErr.Message))
			return
		}
		w.WriteHeader(backendErr.StatusCode)
		return
	}

	writeSwobuError(w, canonical.InternalError("request handling failed"))
}

func writeSwobuError(w http.ResponseWriter, err canonical.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCodeForSwobuError(err.Code))
	errorBody := map[string]any{
		"code":    err.Code,
		"message": err.Message,
		"origin":  err.Origin,
	}
	if len(err.Details) > 0 {
		errorBody["details"] = err.Details
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": errorBody,
	})
}

func statusCodeForSwobuError(code canonical.ErrorCode) int {
	switch code {
	case canonical.ErrorCodeInternal:
		return http.StatusInternalServerError
	case canonical.ErrorCodeBadRequest:
		return http.StatusBadRequest
	case canonical.ErrorCodeUnknownTarget:
		return http.StatusBadRequest
	case canonical.ErrorCodeBadEndpoint:
		return 502
	case canonical.ErrorCodeUnsupportedEndpoint, canonical.ErrorCodeUnsupportedOperation, canonical.ErrorCodeUnsupportedDelivery:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
