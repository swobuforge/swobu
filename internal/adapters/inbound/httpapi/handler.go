package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/platform/httpcontent"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

const (
	maxCompressedRequestBodyBytes int64 = 2 << 20
	maxDecodedRequestBodyBytes    int64 = 8 << 20
)

type requestIngress interface {
	HandleRequest(context.Context, exchange.RequestInput) (exchange.RequestOutput, error)
}

type modelsHandler interface {
	ListModels(context.Context, exchange.ListModelsInput) (exchange.ListModelsOutput, error)
}

type Handler struct {
	requestIngress requestIngress
}

func NewHandler(requestIngress requestIngress) Handler {
	return Handler{requestIngress: requestIngress}
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	writer := &committingResponseWriter{ResponseWriter: w, gate: exchange.NewCommitGate()}
	endpointName, operationPath, err := splitProtocolPath(r.URL.Path)
	if err != nil {
		writeSwobuError(writer, canonical.UnsupportedEndpoint("unsupported endpoint URL"))
		return
	}
	if operationPath == "" {
		writeSwobuError(writer, canonical.UnsupportedEndpoint("protocol operation path is required"))
		return
	}

	endpoint, err := endpointintent.ParseEndpointName(endpointName)
	if err != nil {
		writeSwobuError(writer, canonical.BadEndpoint("endpoint name is invalid"))
		return
	}

	normalizedPath, err := canonical.NormalizePath(operationPath)
	if err != nil {
		writeExchangeError(writer, err)
		return
	}
	if websocketUpgrade(r) {
		if normalizedPath == canonical.NormalizedPathResponses {
			h.serveResponsesWebsocket(writer, r, endpointName, normalizedPath)
			return
		}
		writeExchangeError(writer, canonical.UnsupportedEndpoint("websocket client transport is supported only on protocol /responses routes"))
		return
	}
	if normalizedPath == canonical.NormalizedPathModels {
		h.serveModelsEndpoint(writer, r, endpoint)
		return
	}
	if err := canonical.ValidateClientTransport(r.Method, normalizedPath, false); err != nil {
		writeExchangeError(writer, err)
		return
	}

	hasMessagesProtocolMarker := strings.TrimSpace(r.Header.Get("anthropic-version")) != "" // swobu:io-string source=boundary
	family, err := canonical.InferClientFamily(r.Method, normalizedPath, hasMessagesProtocolMarker)
	if err != nil {
		writeExchangeError(writer, err)
		return
	}

	requestBody, err := decodeRequestBody(w, r)
	if err != nil {
		writeExchangeError(writer, err)
		return
	}
	clientHandler := trafficevidence.NormalizeClientHandler(r.Header.Get("User-Agent"))
	timing := trafficevidence.NewUnknownTiming()
	timing.MarkStarted(time.Now())
	writer.timing = &timing

	requestID := requestIDFromRequest(r)
	logClientRequestShape(requestID, endpoint.String(), family, normalizedPath)

	if h.requestIngress == nil {
		writeSwobuError(writer, canonical.InternalError("exchange ingress is not configured"))
		return
	}

	out, err := h.requestIngress.HandleRequest(r.Context(), exchange.RequestInput{
		EndpointName:    endpoint,
		Request:         newTransportRequest(r.Method, operationPath, r.Header, requestBody),
		ClientHandler:   clientHandler,
		ClientFamily:    family,
		ResponseFraming: delivery.FramingSSE,
		Timing:          &timing,
		ExchangeID:      requestID,
	})
	if err != nil {
		logRequestOutcome(requestID, endpoint.String(), family, normalizedPath, err)
		writeExchangeError(writer, err)
		finalizeTrafficEvidence(r.Context(), requestID, endpoint.String(), family, normalizedPath, out, &timing, err)
		return
	}
	writeModelResolutionHeaders(writer)
	logRequestOutcome(requestID, endpoint.String(), family, normalizedPath, nil)

	if err := writeSuccessResponse(r.Context(), writer, requestID, family, out); err != nil {
		if writer.committed {
			slog.Warn("protocol response write failed after commit",
				"component", "httpapi",
				"event", "response_write_after_commit_failed",
				"request_id", requestID,
				"endpoint", endpoint.String(),
				"ingress_family", string(family),
				"normalized_op", string(normalizedPath),
				"error", err,
			)
			finalizeTrafficEvidence(r.Context(), requestID, endpoint.String(), family, normalizedPath, out, &timing, err)
			return
		}
		writeExchangeError(writer, err)
		finalizeTrafficEvidence(r.Context(), requestID, endpoint.String(), family, normalizedPath, out, &timing, err)
		return
	}
	finalizeTrafficEvidence(r.Context(), requestID, endpoint.String(), family, normalizedPath, out, &timing, nil)
}

func (h Handler) serveModelsEndpoint(w http.ResponseWriter, r *http.Request, endpoint endpointintent.EndpointName) {
	if r.Method != http.MethodGet {
		writeSwobuError(w, canonical.UnsupportedOperation("models endpoint only supports GET"))
		return
	}
	if h.requestIngress == nil {
		writeSwobuError(w, canonical.InternalError("exchange ingress is not configured"))
		return
	}
	m, ok := h.requestIngress.(modelsHandler)
	if !ok {
		writeSwobuError(w, canonical.InternalError("models query is not configured"))
		return
	}
	out, err := m.ListModels(r.Context(), exchange.ListModelsInput{EndpointName: endpoint})
	if err != nil {
		writeExchangeError(w, err)
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

func newTransportRequest(method string, url string, header http.Header, body []byte) transportpkg.TransportRequest {
	return transportpkg.TransportRequest{
		Method: method,
		URL:    url,
		Header: header.Clone(),
		Body:   io.NopCloser(bytes.NewReader(append([]byte(nil), body...))),
	}
}

func logClientRequestShape(
	requestID string,
	endpoint string,
	family canonical.ClientFamily,
	normalizedPath canonical.NormalizedPath,
) {
	slog.Debug("protocol client request",
		"component", "httpapi",
		"event", "ingress_request_shape",
		"request_id", requestID,
		"endpoint", endpoint,
		"ingress_family", string(family),
		"normalized_op", string(normalizedPath),
	)
}

func logRequestOutcome(
	requestID string,
	endpoint string,
	family canonical.ClientFamily,
	normalizedPath canonical.NormalizedPath,
	err error,
) {
	result := "success"
	statusCode := http.StatusOK
	errorOrigin := ""
	backendRef := ""
	errorMessage := ""
	errorCode := ""
	if err != nil {
		errorMessage = err.Error()
		result = "swobu_error"
		errorOrigin = string(canonical.ErrorOriginSwobu)
		var backendErr canonical.BackendError
		if errors.As(err, &backendErr) {
			result = "backend_error"
			statusCode = backendErr.StatusCode
			errorOrigin = string(canonical.ErrorOriginBackend)
			backendRef = strings.TrimSpace(backendErr.BackendRef) // swobu:io-string source=boundary
		} else {
			statusCode = statusCodeForExchangeError(err)
			var swobuErr canonical.Error
			if errors.As(err, &swobuErr) {
				errorCode = string(swobuErr.Code)
			}
		}
	}
	attrs := []any{
		"component", "httpapi",
		"event", "request_outcome",
		"request_id", requestID,
		"endpoint", endpoint,
		"ingress_family", string(family),
		"normalized_op", string(normalizedPath),
		"result", result,
		"status_code", statusCode,
		"error_origin", errorOrigin,
		"backend_ref", backendRef,
	}
	if errorMessage != "" {
		attrs = append(attrs, "error_message", errorMessage)
	}
	if errorCode != "" {
		attrs = append(attrs, "error_code", errorCode)
	}
	if err != nil {
		var swobuErr canonical.Error
		if errors.As(err, &swobuErr) && len(swobuErr.Details) > 0 {
			keys := make([]string, 0, len(swobuErr.Details))
			for key := range swobuErr.Details {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				value := strings.TrimSpace(swobuErr.Details[key]) // swobu:io-string source=boundary
				if value == "" {
					continue
				}
				attrs = append(attrs, "error_detail_"+key, value)
			}
		}
	}
	slog.Debug("protocol request outcome", attrs...)
}

func statusCodeForExchangeError(err error) int {
	var swobuErr canonical.Error
	if errors.As(err, &swobuErr) {
		return statusCodeForSwobuError(swobuErr.Code)
	}
	return http.StatusInternalServerError
}

func writeExchangeError(w http.ResponseWriter, err error) {
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

	writeSwobuError(w, canonical.InternalError("internal server error"))
}

type committingResponseWriter struct {
	http.ResponseWriter
	committed bool
	gate      *exchange.CommitGate
	timing    *trafficevidence.Timing
	firstByte bool
}

func (w *committingResponseWriter) markFirstByte() {
	if w.timing == nil || w.firstByte {
		return
	}
	w.timing.MarkFirstByte(time.Now())
	w.firstByte = true
}

func (w *committingResponseWriter) WriteHeader(statusCode int) {
	w.markFirstByte()
	w.committed = true
	if w.gate != nil {
		w.gate.Commit()
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *committingResponseWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.markFirstByte()
	}
	w.committed = true
	if w.gate != nil {
		w.gate.Commit()
	}
	return w.ResponseWriter.Write(p)
}

func finalizeTrafficEvidence(ctx context.Context, requestID string, endpoint string, family canonical.ClientFamily, normalizedPath canonical.NormalizedPath, out exchange.RequestOutput, timing *trafficevidence.Timing, writeErr error) {
	if timing != nil {
		timing.MarkEnded(time.Now())
	}
	if out.CommitTrafficEvent == nil {
		return
	}
	if err := out.CommitTrafficEvent(ctx, writeErr); err != nil {
		slog.Warn("traffic evidence commit failed",
			"component", "httpapi",
			"event", "traffic_evidence_commit_failed",
			"request_id", requestID,
			"endpoint", endpoint,
			"ingress_family", string(family),
			"normalized_op", string(normalizedPath),
			"error", err,
		)
	}
}

func (w *committingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *committingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *committingResponseWriter) CloseNotify() <-chan bool {
	closeNotifier, ok := w.ResponseWriter.(http.CloseNotifier)
	if !ok {
		return nil
	}
	return closeNotifier.CloseNotify()
}

func (w *committingResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
