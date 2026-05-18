package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/net/websocket"

	responses "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/responses"
	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/ports"
)

const websocketRequestTypeResponseCreate = "response.create"
const maxWebsocketRequestBodyBytes = 1 << 20

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
	if h.requests == nil {
		_ = websocket.Message.Send(conn, string(websocketErrorEvent(canonical.InternalError("request orchestrator is not configured"))))
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
	request, streaming, err := decodeCanonicalRequest(canonical.IngressFamilyResponses, payload)
	if err != nil {
		return err
	}
	requestID := requestIDFromRequest(r)
	out, err := h.requests.Handle(r.Context(), requestpath.HandleInput{
		EndpointName: endpoint,
		RequestID:    requestID,
		Request:      request,
		Contract:     requestpath.NewExecutionContract(streaming),
		Provenance:   ingressProvenance(r, canonical.IngressFamilyResponses, normalizedPath),
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Response.Close()
	}()
	return writeResponsesWebsocketSuccess(conn, requestID, out.Response, streaming)
}

func writeResponsesWebsocketSuccess(conn *websocket.Conn, requestID string, resp ports.ProviderResponse, streaming bool) error {
	envelope := resp.EnvelopeStream()
	if streaming {
		if envelope == nil {
			return canonical.InternalError("streaming provider response is missing a canonical envelope stream")
		}
		return writeResponsesWebsocketEnvelope(conn, requestID, envelope)
	}
	if !streaming {
		if envelope == nil {
			return canonical.InternalError("buffered provider response is missing a canonical envelope stream")
		}
		output, err := projectBufferedOutputFromEnvelope(envelope)
		if err != nil {
			return err
		}
		if output == nil {
			return canonical.InternalError("buffered provider response is missing a canonical output")
		}
		envelope, err = canonical.EventReaderFromCanonicalOutput("ws_buffered_response", output)
		if err != nil {
			return canonical.InternalError("buffered provider response could not be synthesized into envelope events")
		}
		defer func() {
			_ = envelope.Close(context.Background())
		}()
		return writeResponsesWebsocketEnvelope(conn, requestID, envelope)
	}
	return canonical.UnsupportedDelivery("response delivery variant is not implemented")
}

func writeResponsesWebsocketEnvelope(conn *websocket.Conn, requestID string, envelope canonical.EventReader) error {
	if envelope == nil {
		return canonical.InternalError("streaming provider response is missing an output event stream")
	}
	stats, err := drainEncodedFramesWithStats(context.Background(), envelope, responses.NewWireEnvelopeStreamEncoder(), websocketFrameSink{conn: conn})
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

type websocketFrameSink struct {
	conn *websocket.Conn
}

func (s websocketFrameSink) WriteFrame(frame []byte) error {
	if err := websocket.Message.Send(s.conn, string(frame)); err != nil {
		return canonical.InternalError("websocket response write failed")
	}
	return nil
}

func (s websocketFrameSink) Flush() error { return nil }

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

	var compatErr canonical.Error
	if errors.As(err, &compatErr) {
		dto.StatusCode = statusCodeForSwobuError(compatErr.Code)
		dto.Error.Code = string(compatErr.Code)
		dto.Error.Message = compatErr.Message
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
