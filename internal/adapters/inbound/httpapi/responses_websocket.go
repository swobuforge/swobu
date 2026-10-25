package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/admission"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/routing"
)

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
	// Public Workspace upgrades have already passed Workspace publication and bearer
	// admission. Their authority must not depend on a loopback relay peer.
	if workspace, err := meteredWorkspace(request); err == nil {
		if identity, err := WorkspaceServiceAuthorityFromContext(request.Context(), workspace, routing.ServiceModels); err == nil && identity != accessidentity.Local {
			return nil
		}
	}
	if ValidateLocalAdmission(request) != nil {
		return errResponsesWebsocketForbidden
	}
	return nil
}

// websocketConnectionWriter serializes output from concurrent named lanes.
// The Responses codec already owns the event payload; only stream_id is a
// WebSocket transport field and is attached here, after encoding.
type websocketConnectionWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *websocketConnectionWriter) send(lane string, frame []byte) error {
	if lane != "" {
		var event map[string]json.RawMessage
		if err := json.Unmarshal(frame, &event); err != nil || event == nil {
			return canonical.InternalError("WebSocket event is not a JSON object")
		}
		encodedLane, _ := json.Marshal(lane)
		event["stream_id"] = encodedLane
		var err error
		frame, err = json.Marshal(event)
		if err != nil {
			return canonical.InternalError("WebSocket event could not be encoded")
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := websocket.Message.Send(w.conn, string(frame)); err != nil {
		return canonical.InternalError("websocket response write failed")
	}
	return nil
}

type websocketCompleted struct {
	lane string
	err  error
}

func (h Handler) runResponsesWebsocket(conn *websocket.Conn, r *http.Request, endpointName string, normalizedPath canonical.NormalizedPath) {
	conn.MaxPayloadBytes = maxOperatorJSONBodyBytes
	output := &websocketConnectionWriter{conn: conn}
	if h.requestIngress == nil {
		_ = output.send("", websocketErrorEvent(canonical.InternalError("exchange ingress is not configured")))
		return
	}

	parsedWorkspace, err := routing.ParseWorkspaceSlug(endpointName)
	if err != nil {
		_ = output.send("", websocketErrorEvent(canonical.BadEndpoint("endpoint name is invalid")))
		return
	}
	transportHeaders, connectionThreadID, err := ingressTransportRequest(http.MethodPost, string(normalizedPath), parsedWorkspace.String(), canonical.ClientFamilyResponses, r.Header, nil)
	if err != nil {
		_ = output.send("", websocketErrorEvent(err))
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	incoming := make(chan string, 1)
	readErrors := make(chan error, 1)
	readerDone := make(chan struct{})
	completed := make(chan websocketCompleted, maxResponsesWebsocketActive)
	var workers sync.WaitGroup
	go func() {
		defer close(readerDone)
		for {
			var message string
			if err := websocket.Message.Receive(conn, &message); err != nil {
				select {
				case readErrors <- err:
				case <-ctx.Done():
				}
				cancel()
				return
			}
			select {
			case incoming <- message:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() {
		cancel()
		_ = conn.Close()
		<-readerDone
		workers.Wait()
	}()

	request := r.WithContext(ctx)
	queue := &websocketLaneScheduler{}
	startReady := func() {
		for {
			create, ok := queue.next()
			if !ok {
				return
			}
			workers.Add(1)
			go func(create websocketCreate) {
				defer workers.Done()
				err := h.handleResponsesWebsocketCreate(output, create.lane, request, parsedWorkspace, normalizedPath, transportHeaders.Header, connectionThreadID, create.body)
				select {
				case completed <- websocketCompleted{lane: create.lane, err: err}:
				case <-ctx.Done():
				}
			}(create)
		}
	}
	for {
		select {
		case message := <-incoming:
			create, err := parseWebsocketCreate([]byte(message))
			if err != nil {
				if errors.Is(err, errWebsocketEventUnsupported) || errors.Is(err, errWebsocketWarmupUnsupported) {
					err = canonical.NotImplemented(err.Error())
				} else {
					err = canonical.BadRequest(err.Error())
				}
				if output.send(create.lane, websocketErrorEvent(err)) != nil {
					return
				}
				continue
			}
			if err := queue.enqueue(create); err != nil {
				if output.send(create.lane, websocketErrorEvent(canonical.BadRequest(err.Error()))) != nil {
					return
				}
				continue
			}
			startReady()
		case done := <-completed:
			queue.finish(done.lane)
			if done.err != nil && ctx.Err() == nil {
				if output.send(done.lane, websocketErrorEvent(done.err)) != nil {
					return
				}
			}
			startReady()
		case err := <-readErrors:
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				_ = output.send("", websocketErrorEvent(canonical.BadRequest("websocket payload could not be read")))
			}
			return
		case <-ctx.Done():
			return
		}
	}
}

// Each named lane uses precisely the existing exchange and admission path.
// No session credentials, durable conversation cache or separate routing
// authority are introduced by multiplexing.
func (h Handler) handleResponsesWebsocketCreate(output *websocketConnectionWriter, lane string, r *http.Request, workspace routing.WorkspaceSlug, normalizedPath canonical.NormalizedPath, transportHeaders http.Header, executionAffinity executionaffinity.Key, payload []byte) error {
	accessID, err := authorityFromRequest(r, workspace)
	if err != nil {
		return canonical.BadRequest("Access credential is no longer authorized for this Workspace")
	}
	if int64(len(payload)) > maxOperatorJSONBodyBytes {
		return canonical.BadRequest("websocket request payload exceeds maximum allowed size")
	}
	if retry, err := AdmitWorkspaceServiceOperation(r.Context(), workspace, routing.ServiceModels); err != nil {
		if errors.Is(err, accessstate.ErrRequestRateLimited) {
			return canonical.RequestRateLimited("Principal Models request limit exceeded", retry)
		}
		return canonical.BadRequest("Workspace authorization changed before WebSocket operation")
	}
	release, retry, limitErr := AdmitWorkspaceOperationalOperation(r.Context(), workspace, routing.ServiceModels)
	if limitErr != nil {
		if errors.Is(limitErr, admission.ErrExhausted) {
			return canonical.RequestRateLimited("Workspace Models operational limit exceeded", retry)
		}
		return canonical.InternalError("Workspace operational admission unavailable")
	}
	defer release()
	requestID := newRequestID()
	timing := observation.NewUnknownTiming()
	timing.MarkStarted(time.Now())
	out, err := h.requestIngress.HandleRequest(r.Context(), exchange.RequestInput{
		AccessID:          accessID,
		Workspace:         workspace,
		Request:           newTransportRequest(http.MethodPost, string(normalizedPath), transportHeaders, payload),
		ClientFamily:      clientfamily.Classify(r.Header.Get("User-Agent")),
		Operation:         canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses},
		ResponseFraming:   delivery.FramingWebSocket,
		Timing:            &timing,
		ExecutionAffinity: executionAffinity,
		ClientRequestID:   requestID,
	})
	if err != nil {
		h.finalizeDelivery(out, &timing, delivery.Result{Kind: delivery.ExchangeFailed, Err: err}, nil)
		return err
	}
	result := writeResponsesWebsocketSuccess(r.Context(), output, lane, requestID, out.Response, &timing)
	h.finalizeDelivery(out, &timing, result, nil)
	return result.Err
}

func writeResponsesWebsocketSuccess(ctx context.Context, output *websocketConnectionWriter, lane, requestID string, response exchange.ClientResponse, timing *observation.Timing) delivery.Result {
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
	err := writeResponsesWebsocketStream(ctx, output, lane, requestID, transportResponse, timing)
	if err == nil {
		return delivery.Result{Kind: delivery.Succeeded}
	}
	var writeErr websocketFrameWriteError
	if errors.As(err, &writeErr) {
		return classifyClientWriteFailure(ctx, writeErr.err, nil)
	}
	return classifyDeliveryFailure(ctx, nil, err, nil)
}

func writeResponsesWebsocketStream(ctx context.Context, output *websocketConnectionWriter, lane, requestID string, response carrier.MessageTransportResponse, timing *observation.Timing) error {
	stats, err := drainWebsocketMessagesWithStats(ctx, response.Messages, &websocketFrameSink{write: func(frame []byte) error { return output.send(lane, frame) }, timing: timing})
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
	write     func([]byte) error
	timing    *observation.Timing
	firstByte bool
}

func (s *websocketFrameSink) WriteFrame(frame []byte) error {
	if s.timing != nil && !s.firstByte && len(frame) > 0 {
		s.timing.MarkFirstByte(time.Now())
		s.firstByte = true
	}
	return s.write(frame)
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
		dto.StatusCode = statusCodeForBackendError(backendErr)
		dto.Error.Code = "provider_error"
		dto.Error.Message = http.StatusText(dto.StatusCode)
		if backendErr.ProviderError != nil {
			detail := backendErr.ProviderError
			dto.Error.Code = detail.Code
			if dto.Error.Code == "" {
				dto.Error.Code = detail.Type
			}
			if dto.Error.Code == "" {
				dto.Error.Code = "provider_error"
			}
			if detail.Message != "" {
				dto.Error.Message = detail.Message
			}
		}
		raw, _ := json.Marshal(dto)
		return raw
	}

	raw, _ := json.Marshal(dto)
	return raw
}
