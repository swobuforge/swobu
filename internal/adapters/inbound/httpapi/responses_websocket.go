package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/routing"
)

const websocketRequestTypeResponseCreate = "response.create"

var errResponsesWebsocketForbidden = errors.New("responses websocket access is forbidden")

type streamDrainCounters struct {
	EventCount int
	FrameCount int
	FrameBytes int
}

func (h Handler) serveResponsesWebsocket(w http.ResponseWriter, r *http.Request, endpointName string, normalizedPath canonical.NormalizedPath) {
	server := websocket.Server{
		Handshake: func(_ *websocket.Config, request *http.Request) error {
			return validateResponsesWebsocketAccess(request)
		},
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			defer func() {
				_ = conn.Close()
			}()
			h.runResponsesWebsocket(conn, r, endpointName, normalizedPath)
		}),
	}
	server.ServeHTTP(w, r)
}

func validateResponsesWebsocketAccess(request *http.Request) error {
	peerHost, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr)) // swobu:io-string source=boundary
	if err != nil {
		return errResponsesWebsocketForbidden
	}
	peer, err := netip.ParseAddr(peerHost)
	if err != nil || !peer.IsLoopback() {
		return errResponsesWebsocketForbidden
	}

	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	requestAuthority, ok := normalizedLoopbackAuthority(request.Host, scheme)
	if !ok {
		return errResponsesWebsocketForbidden
	}

	origins := request.Header.Values("Origin")
	if len(origins) == 0 {
		return nil
	}
	if len(origins) != 1 {
		return errResponsesWebsocketForbidden
	}
	rawOrigin := strings.TrimSpace(origins[0]) // swobu:io-string source=boundary
	if rawOrigin == "" || strings.EqualFold(rawOrigin, "null") {
		return errResponsesWebsocketForbidden
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil ||
		!strings.EqualFold(origin.Scheme, scheme) ||
		origin.Host == "" ||
		origin.User != nil ||
		origin.Opaque != "" ||
		origin.Path != "" ||
		origin.RawPath != "" ||
		origin.RawQuery != "" ||
		origin.ForceQuery ||
		origin.Fragment != "" {
		return errResponsesWebsocketForbidden
	}
	originAuthority, ok := normalizedLoopbackAuthority(origin.Host, scheme)
	if !ok || originAuthority != requestAuthority {
		return errResponsesWebsocketForbidden
	}
	return nil
}

func normalizedLoopbackAuthority(authority string, scheme string) (string, bool) {
	raw := strings.TrimSpace(authority) // swobu:io-string source=boundary
	if raw == "" || strings.Contains(raw, "%") {
		return "", false
	}
	parsed, err := url.Parse("//" + raw)
	if err != nil ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.Path != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		strings.HasSuffix(parsed.Host, ":") {
		return "", false
	}

	host := parsed.Hostname()
	switch {
	case strings.EqualFold(host, "localhost"):
		host = "localhost"
	default:
		address, parseErr := netip.ParseAddr(host)
		if parseErr != nil || !address.IsLoopback() {
			return "", false
		}
		host = address.String()
	}

	port := parsed.Port()
	if port == "" {
		switch strings.ToLower(scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", false
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(portNumber)), true
}

func (h Handler) runResponsesWebsocket(conn *websocket.Conn, r *http.Request, endpointName string, normalizedPath canonical.NormalizedPath) {
	conn.MaxPayloadBytes = maxOperatorJSONBodyBytes
	if h.requestIngress == nil {
		_ = websocket.Message.Send(conn, string(websocketErrorEvent(canonical.InternalError("exchange ingress is not configured"))))
		return
	}

	parsedWorkspace, err := routing.ParseWorkspaceSlug(endpointName)
	if err != nil {
		_ = websocket.Message.Send(conn, string(websocketErrorEvent(canonical.BadEndpoint("endpoint name is invalid"))))
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	incoming := make(chan string, 1)
	readErrors := make(chan error, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			var message string
			if err := websocket.Message.Receive(conn, &message); err != nil {
				cancel()
				readErrors <- err
				return
			}
			select {
			case incoming <- message:
			default:
				cancel()
				readErrors <- canonical.BadRequest("websocket response.create requests must be processed serially")
				return
			}
		}
	}()
	defer func() {
		cancel()
		_ = conn.Close()
		<-readerDone
	}()

	connectionID := requestIDFromRequest(r)
	var messageSequence uint64
	for {
		var message string
		select {
		case message = <-incoming:
		case err := <-readErrors:
			if errors.Is(err, io.EOF) {
				return
			}
			if ctx.Err() == nil {
				_ = websocket.Message.Send(conn, string(websocketErrorEvent(canonical.BadRequest("websocket payload could not be read"))))
			}
			return
		case <-ctx.Done():
			return
		}

		messageSequence++
		exchangeID := connectionID + "/create/" + strconv.FormatUint(messageSequence, 10)
		request := r.WithContext(ctx)
		if err := h.handleResponsesWebsocketMessage(conn, request, parsedWorkspace, normalizedPath, exchangeID, []byte(message)); err != nil && ctx.Err() == nil {
			_ = websocket.Message.Send(conn, string(websocketErrorEvent(err)))
		}
	}
}

func (h Handler) handleResponsesWebsocketMessage(conn *websocket.Conn, r *http.Request, workspace routing.WorkspaceSlug, normalizedPath canonical.NormalizedPath, requestID string, raw []byte) error {
	if int64(len(raw)) > maxOperatorJSONBodyBytes {
		return canonical.BadRequest("websocket request payload exceeds maximum allowed size")
	}
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" {
		return canonical.BadRequest("websocket request payload is empty")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return canonical.BadRequest("websocket request body is invalid JSON")
	}
	var requestType string
	if t, ok := envelope["type"]; ok {
		_ = json.Unmarshal(t, &requestType)
	}
	if strings.TrimSpace(requestType) != websocketRequestTypeResponseCreate { // swobu:io-string source=boundary
		return canonical.NotImplemented("Swobu cannot yet process this Responses websocket request type")
	}
	delete(envelope, "type")
	payload, err := json.Marshal(envelope)
	if err != nil {
		return canonical.BadRequest("websocket request body is invalid JSON")
	}
	timing := trafficevidence.NewUnknownTiming()
	timing.MarkStarted(time.Now())
	out, err := h.requestIngress.HandleRequest(r.Context(), exchange.RequestInput{
		Workspace:       workspace,
		Request:         newTransportRequest(http.MethodPost, string(normalizedPath), r.Header, payload),
		ClientHandler:   trafficevidence.NormalizeClientHandler(r.Header.Get("User-Agent")),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingWebSocket,
		Timing:          &timing,
		ExchangeID:      requestID,
	})
	if err != nil {
		_ = websocket.Message.Send(conn, string(websocketErrorEvent(err)))
		h.finalizeTrafficEvidence(r.Context(), requestID, workspace.String(), canonical.ClientFamilyResponses, normalizedPath, out, &timing, delivery.Result{Kind: delivery.ExchangeFailed, Err: err})
		return err
	}
	result := writeResponsesWebsocketSuccess(r.Context(), conn, requestID, out.Response, &timing)
	h.finalizeTrafficEvidence(r.Context(), requestID, workspace.String(), canonical.ClientFamilyResponses, normalizedPath, out, &timing, result)
	return result.Err
}

func writeResponsesWebsocketSuccess(ctx context.Context, conn *websocket.Conn, requestID string, response exchange.ClientResponse, timing *trafficevidence.Timing) delivery.Result {
	streaming, ok := response.(exchange.MessageStreamingResponse)
	if !ok {
		return delivery.Result{Kind: delivery.ProviderStreamFailed, Err: canonical.InternalError("websocket response is not message-oriented streaming output")}
	}
	transportResponse := streaming.Response
	if transportResponse.Header.Get("Content-Type") != "application/json" {
		return delivery.Result{Kind: delivery.ProviderStreamFailed, Err: canonical.InternalError("websocket response does not use websocket framing")}
	}
	if transportResponse.Messages == nil {
		return delivery.Result{Kind: delivery.ProviderStreamFailed, Err: canonical.InternalError("streaming client response is missing message stream")}
	}
	err := writeResponsesWebsocketStream(ctx, conn, requestID, transportResponse, timing)
	if err == nil {
		return delivery.Result{Kind: delivery.Succeeded}
	}
	var writeErr websocketFrameWriteError
	if errors.As(err, &writeErr) {
		return classifyClientWriteFailure(ctx, writeErr.err, nil)
	}
	return classifyDeliveryFailure(ctx, nil, err, nil)
}

func writeResponsesWebsocketStream(ctx context.Context, conn *websocket.Conn, requestID string, response carrier.MessageTransportResponse, timing *trafficevidence.Timing) error {
	stats, err := drainWebsocketMessagesWithStats(ctx, response.Messages, &websocketFrameSink{conn: conn, timing: timing})
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	slog.Debug("protocol websocket stream emitted",
		"component", "httpapi",
		"event", "ws_stream_emit_complete",
		"request_id", requestID,
		"event_count", stats.EventCount,
		"frame_count", stats.FrameCount,
		"frame_bytes", stats.FrameBytes,
	)
	return nil
}

type websocketFrameWriter interface {
	WriteFrame([]byte) error
}

func drainWebsocketMessagesWithStats(ctx context.Context, messages carrier.MessageStream, sink websocketFrameWriter) (streamDrainCounters, error) {
	defer func() { _ = messages.Close(ctx) }()
	stats := streamDrainCounters{}
	for {
		message, err := messages.Next(ctx)
		if len(message) > 0 {
			if writeErr := sink.WriteFrame(message); writeErr != nil {
				return stats, websocketFrameWriteError{err: writeErr}
			}
			stats.EventCount++
			stats.FrameCount++
			stats.FrameBytes += len(message)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return stats, nil
			}
			return stats, err
		}
	}
}

type websocketFrameWriteError struct{ err error }

func (e websocketFrameWriteError) Error() string { return e.err.Error() }
func (e websocketFrameWriteError) Unwrap() error { return e.err }

type websocketFrameSink struct {
	conn      *websocket.Conn
	timing    *trafficevidence.Timing
	firstByte bool
}

func (s *websocketFrameSink) WriteFrame(frame []byte) error {
	if s.timing != nil && !s.firstByte && len(frame) > 0 {
		s.timing.MarkFirstByte(time.Now())
		s.firstByte = true
	}
	if err := websocket.Message.Send(s.conn, string(frame)); err != nil {
		return canonical.InternalError("websocket response write failed")
	}
	return nil
}

type responsesWebsocketErrorDTO struct {
	Type       string                      `json:"type"`
	StatusCode int                         `json:"status_code"`
	Error      responsesWebsocketErrorBody `json:"error"`
}

type responsesWebsocketErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func websocketErrorEvent(err error) []byte {
	dto := responsesWebsocketErrorDTO{
		Type:       "error",
		StatusCode: http.StatusInternalServerError,
		Error: responsesWebsocketErrorBody{
			Code:    string(canonical.ErrorCodeInternal),
			Message: "request handling failed",
		},
	}

	var swobuErr canonical.Error
	if errors.As(err, &swobuErr) {
		dto.StatusCode = statusCodeForSwobuError(swobuErr.Code)
		dto.Error.Code = string(swobuErr.Code)
		dto.Error.Message = swobuErr.Message
		raw, _ := json.Marshal(dto)
		return raw
	}

	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		dto.StatusCode = backendErr.StatusCode
		dto.Error.Code = "BACKEND_ERROR"
		dto.Error.Message = backendErr.Message
		raw, _ := json.Marshal(dto)
		return raw
	}

	raw, _ := json.Marshal(dto)
	return raw
}
