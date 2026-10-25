package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

type nativeResponseKey struct{}

type nativeResponseCapture struct {
	mu        sync.Mutex
	body      []byte
	mediaType string
	id        json.RawMessage
}

type nativeResponseTransport struct{ next http.RoundTripper }

func (transport nativeResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	capture, ok := request.Context().Value(nativeResponseKey{}).(*nativeResponseCapture)
	if !ok {
		return transport.next.RoundTrip(request)
	}
	if request.GetBody == nil {
		return nil, fmt.Errorf("MCP native request identity unavailable")
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, err
	}
	var envelope struct {
		ID json.RawMessage `json:"id"`
	}
	err = json.NewDecoder(body).Decode(&envelope)
	_ = body.Close()
	if err != nil {
		return nil, err
	}
	response, err := transport.next.RoundTrip(request)
	if err != nil || response == nil {
		return response, err
	}
	capture.mu.Lock()
	capture.id = envelope.ID
	capture.mediaType, _, _ = mime.ParseMediaType(response.Header.Get("Content-Type"))
	capture.mu.Unlock()
	response.Body = &nativeResponseBody{ReadCloser: response.Body, capture: capture}
	return response, nil
}

type nativeResponseBody struct {
	io.ReadCloser
	capture *nativeResponseCapture
}

func (body *nativeResponseBody) Read(buffer []byte) (int, error) {
	count, err := body.ReadCloser.Read(buffer)
	body.capture.mu.Lock()
	if len(body.capture.body)+count > MaxHTTPResponseBytes {
		body.capture.mu.Unlock()
		return 0, fmt.Errorf("MCP native response exceeded capture bound")
	}
	body.capture.body = append(body.capture.body, buffer[:count]...)
	body.capture.mu.Unlock()
	return count, err
}

func (capture *nativeResponseCapture) result(ctx context.Context) (json.RawMessage, error) {
	capture.mu.Lock()
	body := append([]byte(nil), capture.body...)
	mediaType, id := capture.mediaType, capture.id
	capture.mu.Unlock()
	match := func(raw []byte) (json.RawMessage, error) {
		message, err := jsonrpc.DecodeMessage(raw)
		if err != nil {
			return nil, err
		}
		response, ok := message.(*jsonrpc.Response)
		if !ok {
			return nil, nil
		}
		var envelope struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, err
		}
		if !bytes.Equal(bytes.TrimSpace(id), bytes.TrimSpace(envelope.ID)) {
			return nil, nil
		}
		return response.Result, nil
	}
	if mediaType == "application/json" {
		return match(body)
	}
	if mediaType != "text/event-stream" {
		return nil, fmt.Errorf("MCP native response media type unavailable")
	}
	reader := core.NewBoundedSSEReader(io.NopCloser(bytes.NewReader(body)), MaxHTTPResponseBytes)
	defer reader.Close()
	for {
		event, err := reader.Next(ctx)
		if err != nil {
			return nil, err
		}
		if event.Data == "" {
			continue
		}
		result, err := match([]byte(event.Data))
		if err != nil || len(result) > 0 {
			return result, err
		}
	}
}

func nativeResponseMiddleware(next sdk.MethodHandler) sdk.MethodHandler {
	return func(ctx context.Context, method string, request sdk.Request) (sdk.Result, error) {
		if method != "tools/list" && method != "tools/call" {
			return next(ctx, method, request)
		}
		capture := &nativeResponseCapture{}
		result, err := next(context.WithValue(ctx, nativeResponseKey{}, capture), method, request)
		if err != nil {
			return nil, err
		}
		raw, err := capture.result(ctx)
		if err != nil || len(raw) == 0 {
			return nil, fmt.Errorf("MCP exact native response unavailable")
		}
		if err := restoreNativeResponse(result, raw); err != nil {
			return nil, err
		}
		return result, nil
	}
}
