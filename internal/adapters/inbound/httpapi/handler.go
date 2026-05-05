package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/compatibility"
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
	endpointName, operationPath, err := splitCompatibilityPath(r.URL.Path)
	if err != nil {
		writeSwobuError(w, compatibility.UnsupportedEndpoint("unsupported endpoint URL"))
		return
	}
	if operationPath == "" {
		writeSwobuError(w, compatibility.UnsupportedEndpoint("compatibility operation path is required"))
		return
	}

	endpoint, err := endpointintent.ParseEndpointName(endpointName)
	if err != nil {
		writeSwobuError(w, compatibility.BadEndpoint("endpoint name is invalid"))
		return
	}

	normalizedPath, err := compatibility.NormalizePath(operationPath)
	if err != nil {
		writeCompatibilityError(w, err)
		return
	}
	if isWebSocketUpgrade(r) {
		if normalizedPath == compatibility.NormalizedPathResponses {
			h.serveResponsesWebsocket(w, r, endpointName, normalizedPath)
			return
		}
		writeCompatibilityError(w, compatibility.UnsupportedEndpoint("websocket ingress is supported only on compatibility /responses routes"))
		return
	}
	if normalizedPath == compatibility.NormalizedPathModels {
		h.serveModelsEndpoint(w, r, endpoint)
		return
	}
	if err := compatibility.ValidateIngressTransport(r.Method, normalizedPath, false); err != nil {
		writeCompatibilityError(w, err)
		return
	}

	family, err := compatibility.InferFamily(r.Method, normalizedPath, strings.TrimSpace(r.Header.Get("anthropic-version")) != "")
	if err != nil {
		writeCompatibilityError(w, err)
		return
	}

	requestBody, err := decodeRequestBody(w, r)
	if err != nil {
		writeCompatibilityError(w, err)
		return
	}

	request, deliveryMode, err := decodeCanonicalRequest(family, requestBody)
	if err != nil {
		writeCompatibilityError(w, err)
		return
	}

	if h.requests == nil {
		writeSwobuError(w, compatibility.InternalError("request orchestrator is not configured"))
		return
	}

	out, err := h.requests.Handle(r.Context(), requestpath.HandleInput{
		EndpointName: endpoint,
		RequestID:    requestIDFromRequest(r),
		Request:      request,
		Contract:     requestpath.NewExecutionContract(deliveryMode),
		Provenance:   ingressProvenance(r, family, normalizedPath),
	})
	if err != nil {
		writeCompatibilityError(w, err)
		return
	}
	defer func() {
		_ = out.Response.Close()
	}()
	writeModelResolutionHeaders(w, out.Response.Metadata())

	if err := writeSuccessResponse(w, family, out.Response); err != nil {
		writeCompatibilityError(w, err)
	}
}

func (h Handler) serveModelsEndpoint(w http.ResponseWriter, r *http.Request, endpoint endpointintent.EndpointName) {
	if r.Method != http.MethodGet {
		writeSwobuError(w, compatibility.UnsupportedOperation("models endpoint only supports GET"))
		return
	}
	if h.requests == nil {
		writeSwobuError(w, compatibility.InternalError("request orchestrator is not configured"))
		return
	}
	modelsHandler, ok := h.requests.(ModelsHandler)
	if !ok {
		writeSwobuError(w, compatibility.InternalError("models query is not configured"))
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
// It parses endpoint-qualified compatibility routes only; it is not a second
// routing layer and must not absorb family-specific semantics.
func splitCompatibilityPath(raw string) (string, string, error) {
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
			return nil, compatibility.BadRequest("request body exceeds maximum allowed size")
		}
		return nil, compatibility.BadRequest("request body could not be read")
	}
	decoded, err := httpcontent.DecodeBytesLimited(r.Header.Get("Content-Encoding"), raw, maxDecodedRequestBodyBytes)
	if err != nil {
		return nil, compatibility.BadRequest("request content encoding is unsupported, invalid, or exceeds size limits")
	}
	return decoded, nil
}

func decodeCanonicalRequest(family compatibility.IngressFamily, raw []byte) (compatibility.CanonicalRequest, compatibility.DeliveryMode, error) {
	codec, err := codecForFamily(family)
	if err != nil {
		return nil, "", err
	}
	return codec.decodeRequest(raw)
}

func decodeJSONObject(raw json.RawMessage, message string) (map[string]any, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, compatibility.BadRequest(message)
	}
	return out, nil
}

func requestIDFromRequest(r *http.Request) string {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
	if requestID == "" {
		requestID = strings.TrimSpace(r.Header.Get("X-Request-ID"))
	}
	if requestID == "" {
		requestID = newRequestID()
	}
	return requestID
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "swobu-request"
	}
	return hex.EncodeToString(raw[:])
}

func deliveryModeFromStream(stream bool) compatibility.DeliveryMode {
	if stream {
		return compatibility.DeliveryModeStreaming
	}
	return compatibility.DeliveryModeBuffered
}

func isWebSocketUpgrade(r *http.Request) bool {
	if r == nil {
		return false
	}
	connection := strings.ToLower(strings.TrimSpace(r.Header.Get("Connection")))
	upgrade := strings.ToLower(strings.TrimSpace(r.Header.Get("Upgrade")))
	return strings.Contains(connection, "upgrade") && upgrade == "websocket"
}

func writeCompatibilityError(w http.ResponseWriter, err error) {
	var swobuErr compatibility.Error
	if errors.As(err, &swobuErr) {
		writeSwobuError(w, swobuErr)
		return
	}

	var backendErr compatibility.BackendError
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

	writeSwobuError(w, compatibility.InternalError("request handling failed"))
}

func writeSwobuError(w http.ResponseWriter, err compatibility.Error) {
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

func statusCodeForSwobuError(code compatibility.ErrorCode) int {
	switch code {
	case compatibility.ErrorCodeInternal:
		return http.StatusInternalServerError
	case compatibility.ErrorCodeBadRequest:
		return http.StatusBadRequest
	case compatibility.ErrorCodeUnknownTarget:
		return http.StatusBadRequest
	case compatibility.ErrorCodeBadEndpoint:
		return 502
	case compatibility.ErrorCodeUnsupportedEndpoint, compatibility.ErrorCodeUnsupportedOperation, compatibility.ErrorCodeUnsupportedDelivery:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
