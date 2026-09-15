package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/platform/httpcontent"
	"github.com/swobuforge/swobu/internal/routing"
)

const (
	// This is an absolute deployment safety ceiling. Workspace semantic limits
	// are resolved and enforced independently by exchange ingress.
	maxCompressedRequestBodyBytes int64 = 48 << 20
	maxDecodedRequestBodyBytes    int64 = 48 << 20
	clientClosedRequestStatus           = 499
)

type requestIngress interface {
	HandleRequest(context.Context, exchange.RequestInput) (exchange.RequestOutput, error)
}

type modelsHandler interface {
	ListModels(context.Context, exchange.ListModelsInput) (exchange.ListModelsOutput, error)
}

type Handler struct {
	requestIngress  requestIngress
	trafficEvidence observation.TrafficEventSink
}

func NewHandler(requestIngress requestIngress, trafficEvidence observation.TrafficEventSink) Handler {
	return Handler{requestIngress: requestIngress, trafficEvidence: trafficEvidence}
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	writer := &committingResponseWriter{ResponseWriter: w}
	endpointName, operationPath, err := splitProtocolPath(r.URL.Path)
	if err != nil {
		writeSwobuError(writer, canonical.UnsupportedEndpoint("unsupported endpoint URL"))
		return
	}
	if operationPath == "" {
		writeSwobuError(writer, canonical.UnsupportedEndpoint("protocol operation path is required"))
		return
	}

	workspace, err := routing.ParseWorkspaceSlug(endpointName)
	if err != nil {
		writeSwobuError(writer, canonical.BadEndpoint("endpoint name is invalid"))
		return
	}

	normalizedPath, err := canonical.NormalizePath(operationPath)
	if err != nil {
		writeExchangeError(writer, "", err)
		return
	}
	if websocketUpgrade(r) {
		if normalizedPath == canonical.NormalizedPathResponses {
			h.serveResponsesWebsocket(writer, r, endpointName, normalizedPath)
			return
		}
		writeExchangeError(writer, "", canonical.UnsupportedEndpoint("websocket client transport is supported only on protocol /responses routes"))
		return
	}
	if normalizedPath == canonical.NormalizedPathModels {
		h.serveModelsEndpoint(writer, r, workspace)
		return
	}
	if err := canonical.ValidateClientTransport(r.Method, normalizedPath, false); err != nil {
		writeExchangeError(writer, "", err)
		return
	}

	hasMessagesProtocolMarker := strings.TrimSpace(r.Header.Get("anthropic-version")) != "" // swobu:io-string source=boundary
	family, err := canonical.InferClientFamily(r.Method, normalizedPath, hasMessagesProtocolMarker)
	if err != nil {
		writeExchangeError(writer, "", err)
		return
	}

	requestBody, err := decodeRequestBody(w, r)
	if err != nil {
		writeExchangeError(writer, family, err)
		return
	}
	clientHandler := trafficevidence.NormalizeClientHandler(r.Header.Get("User-Agent"))
	timing := trafficevidence.NewUnknownTiming()
	timing.MarkStarted(time.Now())
	writer.timing = &timing

	requestID := requestIDFromRequest(r)
	logClientRequestShape(requestID, workspace.String(), family, normalizedPath)

	if h.requestIngress == nil {
		writeSwobuError(writer, canonical.InternalError("exchange ingress is not configured"))
		return
	}

	transportRequest, executionAffinity, err := ingressTransportRequest(r.Method, operationPath, workspace.String(), r.Header, requestBody)
	if err != nil {
		writeExchangeError(writer, family, err)
		return
	}
	out, err := h.requestIngress.HandleRequest(r.Context(), exchange.RequestInput{
		Workspace:         workspace,
		Request:           transportRequest,
		ClientHandler:     clientHandler,
		ClientFamily:      family,
		ResponseFraming:   delivery.FramingSSE,
		Timing:            &timing,
		ExecutionAffinity: executionAffinity,
		ExchangeID:        requestID,
	})
	if err != nil {
		deliveryResult := exchangeFailureDeliveryResult(err)
		logRequestOutcome(requestID, workspace.String(), family, normalizedPath, out.Target.TargetID, out.AttemptCount, deliveryResult)
		if deliveryResult.Kind == delivery.ClientCancelled {
			h.finalizeTrafficEvidence(r.Context(), requestID, workspace.String(), family, normalizedPath, out, &timing, deliveryResult)
			return
		}
		writeExchangeError(writer, family, err)
		h.finalizeTrafficEvidence(r.Context(), requestID, workspace.String(), family, normalizedPath, out, &timing, deliveryResult)
		return
	}
	writeModelResolutionHeaders(writer)

	deliveryResult := writeSuccessResponse(r.Context(), writer, requestID, family, out)
	if deliveryResult.Kind != delivery.Succeeded {
		err := deliveryResult.Err
		logRequestOutcome(requestID, workspace.String(), family, normalizedPath, out.Target.TargetID, out.AttemptCount, deliveryResult)
		if deliveryResult.Kind == delivery.ClientCancelled {
			h.finalizeTrafficEvidence(r.Context(), requestID, workspace.String(), family, normalizedPath, out, &timing, deliveryResult)
			return
		}
		if writer.committed {
			logResponseWriteAfterCommitFailure(requestID, workspace.String(), family, normalizedPath, out.Target.TargetID, out.AttemptCount, err)
			h.finalizeTrafficEvidence(r.Context(), requestID, workspace.String(), family, normalizedPath, out, &timing, deliveryResult)
			return
		}
		writeExchangeError(writer, family, err)
		h.finalizeTrafficEvidence(r.Context(), requestID, workspace.String(), family, normalizedPath, out, &timing, deliveryResult)
		return
	}
	logRequestOutcome(requestID, workspace.String(), family, normalizedPath, out.Target.TargetID, out.AttemptCount, delivery.Result{Kind: delivery.Succeeded})
	h.finalizeTrafficEvidence(r.Context(), requestID, workspace.String(), family, normalizedPath, out, &timing, deliveryResult)
}

func logResponseWriteAfterCommitFailure(requestID, workspace string, family canonical.ClientFamily, normalizedPath canonical.NormalizedPath, targetID string, attemptCount int, err error) {
	attrs := []any{
		"component", "httpapi", "event", "response_write_after_commit_failed",
		"request_id", requestID, "workspace", workspace, "ingress_family", string(family),
		"normalized_op", string(normalizedPath), "target_id", targetID, "attempt_count", attemptCount,
		"error_type", fmt.Sprintf("%T", err),
	}
	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		attrs = append(attrs, "status_code", statusCodeForBackendError(backendErr))
		if backendErr.SourceProtocol != "" {
			attrs = append(attrs, "source_protocol", backendErr.SourceProtocol.String())
		}
		if backendErr.ProviderError != nil {
			detail := backendErr.ProviderError
			attrs = append(attrs, "backend_error_type", detail.Type, "backend_error_code", detail.Code, "backend_request_id", detail.RequestID)
		}
	}
	slog.LogAttrs(context.Background(), slog.LevelWarn, "protocol response write failed after commit", anyAttrs(attrs)...)
}

func exchangeFailureDeliveryResult(err error) delivery.Result {
	kind := delivery.ExchangeFailed
	if errors.Is(err, context.Canceled) {
		kind = delivery.ClientCancelled
	}
	return delivery.Result{Kind: kind, Err: err}
}

func (h Handler) serveModelsEndpoint(w http.ResponseWriter, r *http.Request, workspace routing.WorkspaceSlug) {
	if r.Method != http.MethodGet {
		writeSwobuError(w, canonical.ClientUnsupportedOperation(
			"models endpoint does not support this method",
			"Change the HTTP method to GET and retry",
		))
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
	out, err := m.ListModels(r.Context(), exchange.ListModelsInput{Workspace: workspace})
	if err != nil {
		writeExchangeError(w, "", err)
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

func newTransportRequest(method string, url string, header http.Header, body []byte) carrier.TransportRequest {
	return carrier.TransportRequest{
		Method: method,
		URL:    url,
		Header: header.Clone(),
		Body:   body,
	}
}

const openCodeSessionHeader = "X-Opencode-Session"

func ingressTransportRequest(method, url, workspace string, header http.Header, body []byte) (carrier.TransportRequest, executionaffinity.Key, error) {
	request := newTransportRequest(method, url, header, body)
	var selected string
	for _, value := range request.Header.Values(openCodeSessionHeader) {
		if strings.TrimSpace(value) != "" {
			selected = value
			break
		}
	}
	request.Header.Del(openCodeSessionHeader)
	if selected == "" {
		return request, executionaffinity.Key{}, nil
	}
	id, err := executionaffinity.Derive("client/x-opencode-session/v1", workspace, selected)
	if err != nil {
		return carrier.TransportRequest{}, executionaffinity.Key{}, canonical.InternalError("client execution affinity could not be derived")
	}
	return request, id, nil
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
	targetID string,
	attemptCount int,
	deliveryResult delivery.Result,
) {
	err := deliveryResult.Err
	result := "success"
	statusCode := http.StatusOK
	errorOrigin := ""
	// RequestOutput is authoritative after selection. A backend error can fill
	// target identity only when orchestration returned no terminal snapshot.
	targetID = strings.TrimSpace(targetID) // swobu:io-string source=boundary
	errorCode := ""
	if err != nil {
		result = "swobu_error"
		errorOrigin = string(canonical.ErrorOriginSwobu)
		if deliveryResult.Kind == delivery.ClientCancelled || errors.Is(err, context.Canceled) {
			result = "canceled"
			statusCode = clientClosedRequestStatus
			errorOrigin = "client"
		} else {
			var backendErr canonical.BackendError
			if errors.As(err, &backendErr) {
				result = "backend_error"
				statusCode = statusCodeForBackendError(backendErr)
				errorOrigin = string(canonical.ErrorOriginBackend)
				if targetID == "" {
					targetID = strings.TrimSpace(backendErr.TargetID) // swobu:io-string source=boundary
				}
			} else {
				statusCode = statusCodeForExchangeError(err)
				// TerminalErrorCode is the single classifier for an unexpected
				// Swobu-owned failure: a typed error reports its own code, an
				// untyped one resolves to INTERNAL_ERROR. It inspects type only;
				// the raw cause stays in traffic evidence, never on this line.
				errorCode = string(canonical.TerminalErrorCode(err))
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
		"target_id", targetID,
	}
	if errorCode != "" {
		attrs = append(attrs, "error_code", errorCode)
	}
	var loggedBackend canonical.BackendError
	if errors.As(err, &loggedBackend) && loggedBackend.SourceProtocol != "" {
		attrs = append(attrs, "source_protocol", loggedBackend.SourceProtocol.String())
		if loggedBackend.ProviderError != nil {
			detail := loggedBackend.ProviderError
			attrs = append(attrs, "backend_error_type", detail.Type, "backend_error_code", detail.Code, "backend_request_id", detail.RequestID)
		}
	}
	attrs = append(attrs, "attempt_count", attemptCount)
	level := slog.LevelDebug
	if err != nil && deliveryResult.Kind != delivery.ClientCancelled && !errors.Is(err, context.Canceled) {
		var backendErr canonical.BackendError
		if errors.As(err, &backendErr) || errorCode != string(canonical.ErrorCodeInternal) {
			level = slog.LevelWarn
		} else {
			level = slog.LevelError
		}
	}
	slog.LogAttrs(context.Background(), level, "protocol request outcome", anyAttrs(attrs)...)
}

func anyAttrs(values []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(values)/2)
	for i := 0; i+1 < len(values); i += 2 {
		attrs = append(attrs, slog.Any(values[i].(string), values[i+1]))
	}
	return attrs
}

func statusCodeForExchangeError(err error) int {
	if errors.Is(err, context.Canceled) {
		return clientClosedRequestStatus
	}
	var swobuErr canonical.Error
	if errors.As(err, &swobuErr) {
		return statusCodeForSwobuError(swobuErr.Code)
	}
	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		return statusCodeForBackendError(backendErr)
	}
	return http.StatusInternalServerError
}

func statusCodeForBackendError(err canonical.BackendError) int {
	if err.StatusCode >= http.StatusBadRequest && err.StatusCode <= 599 {
		return err.StatusCode
	}
	return http.StatusBadGateway
}

func writeExchangeError(w http.ResponseWriter, family canonical.ClientFamily, err error) {
	var swobuErr canonical.Error
	if errors.As(err, &swobuErr) {
		if family == "" {
			writeSwobuError(w, swobuErr)
			return
		}
		writeProtocolError(w, family, statusCodeForSwobuError(swobuErr.Code), "swobu_error", string(swobuErr.Code), swobuErr.Message, "", "")
		return
	}

	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		statusCode := statusCodeForBackendError(backendErr)
		if backendErr.RetryAfterHeaderValue != "" {
			w.Header().Set("Retry-After", backendErr.RetryAfterHeaderValue)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write(projectBackendError(family, statusCode, backendErr))
		return
	}

	internal := canonical.InternalError("internal server error")
	if family == "" {
		writeSwobuError(w, internal)
		return
	}
	writeProtocolError(w, family, http.StatusInternalServerError, "swobu_error", string(internal.Code), internal.Message, "", "")
}

func projectBackendError(family canonical.ClientFamily, statusCode int, backendErr canonical.BackendError) []byte {
	if backendErr.ProviderError != nil && backendErr.SourceProtocol == family && json.Valid([]byte(backendErr.Message)) {
		return []byte(backendErr.Message)
	}
	message := strings.TrimSpace(backendErr.Message) // swobu:io-string source=boundary
	errorType, code, param, requestID := "provider_error", "", "", ""
	if backendErr.ProviderError != nil {
		detail := backendErr.ProviderError
		message, errorType, code, param, requestID = detail.Message, detail.Type, detail.Code, detail.Param, detail.RequestID
	}
	if message == "" {
		message = http.StatusText(statusCode)
	}
	if family == canonical.ClientFamilyMessages {
		projectedType := messagesErrorTypeForStatus(statusCode)
		if backendErr.SourceProtocol == canonical.ClientFamilyMessages && errorType != "" {
			projectedType = errorType
		}
		body := struct {
			Type  string `json:"type"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
			RequestID string `json:"request_id,omitempty"`
		}{Type: "error", RequestID: requestID}
		body.Error.Type, body.Error.Message = projectedType, message
		raw, _ := json.Marshal(body)
		return raw
	}
	if errorType == "" {
		errorType = "provider_error"
	}
	body := struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code,omitempty"`
			Param   string `json:"param,omitempty"`
		} `json:"error"`
	}{}
	body.Error.Message, body.Error.Type, body.Error.Code, body.Error.Param = message, errorType, code, param
	raw, _ := json.Marshal(body)
	return raw
}

func writeProtocolError(w http.ResponseWriter, family canonical.ClientFamily, status int, errorType, code, message, param, requestID string) {
	if family == canonical.ClientFamilyMessages {
		body := struct {
			Type  string `json:"type"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}{Type: "error"}
		body.Error.Type, body.Error.Message = messagesErrorTypeForStatus(status), message
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
		return
	}
	backend := canonical.NewStructuredBackendError("swobu", family, status, canonical.BackendErrorDetail{
		Type: errorType, Code: code, Message: message, Param: param, RequestID: requestID,
	}, "")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(projectBackendError(family, status, backend))
}

func messagesErrorTypeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusPaymentRequired:
		return "billing_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusConflict:
		return "conflict_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusGatewayTimeout:
		return "timeout_error"
	case 529:
		return "overloaded_error"
	default:
		if status >= http.StatusInternalServerError {
			return "api_error"
		}
		return "invalid_request_error"
	}
}

type committingResponseWriter struct {
	http.ResponseWriter
	committed bool
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
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *committingResponseWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.markFirstByte()
	}
	w.committed = true
	return w.ResponseWriter.Write(p)
}

func (h Handler) finalizeTrafficEvidence(ctx context.Context, requestID string, endpoint string, family canonical.ClientFamily, normalizedPath canonical.NormalizedPath, out exchange.RequestOutput, timing *trafficevidence.Timing, result delivery.Result) {
	if timing != nil {
		timing.MarkEnded(time.Now())
	}
	if out.TrafficEvidence != nil && h.trafficEvidence != nil && timing != nil {
		event, err := exchange.BuildTerminalTrafficEvent(out.TrafficEvidence, result, *timing)
		if err == nil {
			h.trafficEvidence.Append(ctx, event)
			return
		}
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
