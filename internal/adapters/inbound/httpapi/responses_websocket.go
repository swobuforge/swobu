package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/net/websocket"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/exchange"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

const websocketRequestTypeResponseCreate = "response.create"
const maxWebsocketRequestBodyBytes = 1 << 20

type streamDrainCounters struct {
	EventCount  int
	FrameCount  int
	FrameBytes  int
	FrameSHA256 string
}

func (h Handler) serveResponsesWebsocket(w http.ResponseWriter, r *http.Request, endpointName string, normalizedPath canonical.NormalizedPath) {
	server := websocket.Server{
		Handshake: nil,
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			defer func() {
				_ = conn.Close()
			}()
			h.runResponsesWebsocket(conn, r, endpointName, normalizedPath)
		}),
	}
	server.ServeHTTP(w, r)
}

func (h Handler) runResponsesWebsocket(conn *websocket.Conn, r *http.Request, endpointName string, normalizedPath canonical.NormalizedPath) {
	conn.MaxPayloadBytes = maxWebsocketRequestBodyBytes
	if h.requestIngress == nil {
		_ = websocket.Message.Send(conn, string(websocketErrorEvent(canonical.InternalError("exchange ingress is not configured"))))
		return
	}

	parsedEndpoint, err := endpointintent.ParseEndpointName(endpointName)
	if err != nil {
		_ = websocket.Message.Send(conn, string(websocketErrorEvent(canonical.BadEndpoint("endpoint name is invalid"))))
		return
	}

	for {
		var message string
		if err := websocket.Message.Receive(conn, &message); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			_ = websocket.Message.Send(conn, string(websocketErrorEvent(canonical.BadRequest("websocket payload could not be read"))))
			return
		}

		if err := h.handleResponsesWebsocketMessage(conn, r, parsedEndpoint, normalizedPath, []byte(message)); err != nil {
			_ = websocket.Message.Send(conn, string(websocketErrorEvent(err)))
		}
	}
}

func (h Handler) handleResponsesWebsocketMessage(conn *websocket.Conn, r *http.Request, endpoint endpointintent.EndpointName, normalizedPath canonical.NormalizedPath, raw []byte) error {
	if len(raw) > maxWebsocketRequestBodyBytes {
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
		return canonical.UnsupportedOperation("websocket request type is not implemented")
	}
	delete(envelope, "type")
	payload, err := json.Marshal(envelope)
	if err != nil {
		return canonical.BadRequest("websocket request body is invalid JSON")
	}
	requestID := requestIDFromRequest(r)
	out, err := h.requestIngress.HandleRequest(r.Context(), exchange.RequestInput{
		EndpointName:    endpoint,
		Request:         newTransportRequest(http.MethodPost, string(normalizedPath), r.Header, payload),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingWebSocket,
	})
	if err != nil {
		return err
	}
	return writeResponsesWebsocketSuccess(conn, requestID, out.Response)
}

func writeResponsesWebsocketSuccess(conn *websocket.Conn, requestID string, response exchange.TransportResponse) error {
	if response.Transport.Header.Get("Content-Type") != "application/json" {
		return canonical.UnsupportedDelivery("websocket responses require websocket-framed streaming output")
	}
	if response.Transport.Body == nil {
		return canonical.InternalError("streaming client response is missing transport body")
	}
	return writeResponsesWebsocketStream(conn, requestID, response.Transport)
}

func writeResponsesWebsocketStream(conn *websocket.Conn, requestID string, response transportpkg.TransportResponse) error {
	stats, err := drainWebsocketBodyWithStats(response.Body, websocketFrameSink{conn: conn})
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return canonical.InternalError("stream decoding failed")
	}
	slog.Debug("protocol websocket stream emitted",
		"component", "httpapi",
		"event", "ws_stream_emit_complete",
		"request_id", requestID,
		"event_count", stats.EventCount,
		"frame_count", stats.FrameCount,
		"frame_bytes", stats.FrameBytes,
		"frame_sha256", stats.FrameSHA256,
	)
	return nil
}

func drainWebsocketBodyWithStats(body io.ReadCloser, sink websocketFrameSink) (streamDrainCounters, error) {
	defer func() { _ = body.Close() }()
	stats := streamDrainCounters{}
	hash := sha256.New()
	buf := make([]byte, 4096)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			if writeErr := sink.WriteFrame(chunk); writeErr != nil {
				return stats, writeErr
			}
			_, _ = hash.Write(chunk)
			stats.EventCount++
			stats.FrameCount++
			stats.FrameBytes += len(chunk)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				stats.FrameSHA256 = hex.EncodeToString(hash.Sum(nil))
				return stats, nil
			}
			return stats, err
		}
	}
}

type websocketFrameSink struct {
	conn *websocket.Conn
}

func (s websocketFrameSink) WriteFrame(frame []byte) error {
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
