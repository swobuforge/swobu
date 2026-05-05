package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/websocket"

	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/ports"
)

const websocketRequestTypeResponseCreate = "response.create"
const maxWebsocketRequestBodyBytes = 1 << 20

func (h Handler) serveResponsesWebsocket(w http.ResponseWriter, r *http.Request, endpointName string, normalizedPath compatibility.NormalizedPath) {
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

func (h Handler) runResponsesWebsocket(conn *websocket.Conn, r *http.Request, endpointName string, normalizedPath compatibility.NormalizedPath) {
	conn.MaxPayloadBytes = maxWebsocketRequestBodyBytes
	if h.requests == nil {
		_ = websocket.Message.Send(conn, string(websocketErrorEvent(compatibility.InternalError("request orchestrator is not configured"))))
		return
	}

	parsedEndpoint, err := endpointintent.ParseEndpointName(endpointName)
	if err != nil {
		_ = websocket.Message.Send(conn, string(websocketErrorEvent(compatibility.BadEndpoint("endpoint name is invalid"))))
		return
	}

	for {
		var message string
		if err := websocket.Message.Receive(conn, &message); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			_ = websocket.Message.Send(conn, string(websocketErrorEvent(compatibility.BadRequest("websocket payload could not be read"))))
			return
		}

		if err := h.handleResponsesWebsocketMessage(conn, r, parsedEndpoint, normalizedPath, []byte(message)); err != nil {
			_ = websocket.Message.Send(conn, string(websocketErrorEvent(err)))
		}
	}
}

func (h Handler) handleResponsesWebsocketMessage(conn *websocket.Conn, r *http.Request, endpoint endpointintent.EndpointName, normalizedPath compatibility.NormalizedPath, raw []byte) error {
	if len(raw) > maxWebsocketRequestBodyBytes {
		return compatibility.BadRequest("websocket request payload exceeds maximum allowed size")
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return compatibility.BadRequest("websocket request payload is empty")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return compatibility.BadRequest("websocket request body is invalid JSON")
	}
	var requestType string
	if t, ok := envelope["type"]; ok {
		_ = json.Unmarshal(t, &requestType)
	}
	if strings.TrimSpace(requestType) != websocketRequestTypeResponseCreate {
		return compatibility.UnsupportedOperation("websocket request type is not implemented")
	}
	delete(envelope, "type")
	payload, err := json.Marshal(envelope)
	if err != nil {
		return compatibility.BadRequest("websocket request body is invalid JSON")
	}
	request, deliveryMode, err := decodeCanonicalRequest(compatibility.IngressFamilyResponses, payload)
	if err != nil {
		return err
	}
	out, err := h.requests.Handle(r.Context(), requestpath.HandleInput{
		EndpointName: endpoint,
		RequestID:    requestIDFromRequest(r),
		Request:      request,
		Contract:     requestpath.NewExecutionContract(deliveryMode),
		Provenance:   ingressProvenance(r, compatibility.IngressFamilyResponses, normalizedPath),
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Response.Close()
	}()
	return writeResponsesWebsocketSuccess(conn, out.Response)
}

func writeResponsesWebsocketSuccess(conn *websocket.Conn, resp ports.ExecuteResponse) error {
	switch resp.DeliveryMode() {
	case compatibility.DeliveryModeStreaming:
		return writeResponsesWebsocketStream(conn, resp.Stream())
	case compatibility.DeliveryModeBuffered:
		return writeResponsesWebsocketBuffered(conn, resp.Output())
	default:
		return compatibility.UnsupportedDelivery("response delivery mode is not implemented")
	}
}

func writeResponsesWebsocketBuffered(conn *websocket.Conn, output compatibility.CanonicalOutput) error {
	if output == nil {
		return compatibility.InternalError("buffered provider response is missing a canonical output")
	}
	events := bufferedOutputToWebsocketEvents(output)
	encoder := newResponsesClientStreamEncoderWire()
	for _, event := range events {
		frames, err := encoder.Encode(event)
		if err != nil {
			return err
		}
		for _, frame := range frames {
			if err := websocket.Message.Send(conn, string(frame)); err != nil {
				return compatibility.InternalError("websocket response write failed")
			}
		}
	}
	tail, err := encoder.Finish()
	if err != nil {
		return err
	}
	for _, frame := range tail {
		if err := websocket.Message.Send(conn, string(frame)); err != nil {
			return compatibility.InternalError("websocket response write failed")
		}
	}
	return nil
}

func writeResponsesWebsocketStream(conn *websocket.Conn, stream compatibility.CanonicalOutputEventStream) error {
	if stream == nil {
		return compatibility.InternalError("streaming provider response is missing an output event stream")
	}
	encoder := newResponsesClientStreamEncoderWire()
	for {
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			tail, tailErr := encoder.Finish()
			if tailErr != nil {
				return tailErr
			}
			for _, frame := range tail {
				if sendErr := websocket.Message.Send(conn, string(frame)); sendErr != nil {
					return compatibility.InternalError("websocket response write failed")
				}
			}
			return nil
		}
		if err != nil {
			return compatibility.InternalError("stream decoding failed")
		}
		frames, err := encoder.Encode(event)
		if err != nil {
			return err
		}
		for _, frame := range frames {
			if err := websocket.Message.Send(conn, string(frame)); err != nil {
				return compatibility.InternalError("websocket response write failed")
			}
		}
	}
}

func bufferedOutputToWebsocketEvents(output compatibility.CanonicalOutput) []compatibility.OutputEvent {
	events := []compatibility.OutputEvent{{
		Kind:     compatibility.OutputEventStarted,
		ResultID: output.ResultID(),
		Model:    output.Model(),
	}}
	for _, item := range output.Items() {
		switch item.Kind {
		case compatibility.ItemKindText:
			events = append(events, compatibility.OutputEvent{
				Kind:     compatibility.OutputEventItemStarted,
				ItemKind: compatibility.OutputItemText,
				ItemID:   item.ItemID,
			})
			events = append(events, compatibility.OutputEvent{
				Kind:      compatibility.OutputEventTextDelta,
				ItemID:    item.ItemID,
				TextDelta: item.Text,
			})
			events = append(events, compatibility.OutputEvent{
				Kind:     compatibility.OutputEventItemCompleted,
				ItemKind: compatibility.OutputItemText,
				ItemID:   item.ItemID,
			})
		case compatibility.ItemKindToolUse:
			events = append(events, compatibility.OutputEvent{
				Kind:      compatibility.OutputEventItemStarted,
				ItemKind:  compatibility.OutputItemToolUse,
				ItemID:    item.ItemID,
				ToolUseID: item.ToolUseID,
				Name:      item.Name,
			})
			input, _ := json.Marshal(item.Input)
			events = append(events, compatibility.OutputEvent{
				Kind:           compatibility.OutputEventToolUseArgumentsDelta,
				ItemKind:       compatibility.OutputItemToolUse,
				ItemID:         item.ItemID,
				ToolUseID:      item.ToolUseID,
				Name:           item.Name,
				ArgumentsDelta: string(input),
			})
			events = append(events, compatibility.OutputEvent{
				Kind:      compatibility.OutputEventItemCompleted,
				ItemKind:  compatibility.OutputItemToolUse,
				ItemID:    item.ItemID,
				ToolUseID: item.ToolUseID,
				Name:      item.Name,
			})
		}
	}
	events = append(events, compatibility.OutputEvent{
		Kind:         compatibility.OutputEventCompleted,
		FinishReason: output.FinishReason(),
	})
	return events
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
			Code:    string(compatibility.ErrorCodeInternal),
			Message: "request handling failed",
		},
	}

	var compatErr compatibility.Error
	if errors.As(err, &compatErr) {
		dto.StatusCode = statusCodeForSwobuError(compatErr.Code)
		dto.Error.Code = string(compatErr.Code)
		dto.Error.Message = compatErr.Message
		raw, _ := json.Marshal(dto)
		return raw
	}

	var backendErr compatibility.BackendError
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
