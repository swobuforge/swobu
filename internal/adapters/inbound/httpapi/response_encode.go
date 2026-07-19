package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

func writeSuccessResponse(ctx context.Context, w http.ResponseWriter, requestID string, family canonical.ClientFamily, out exchange.RequestOutput) transportpkg.DeliveryResult {
	_ = family
	switch response := out.Response.(type) {
	case exchange.BufferedResponse:
		return writeBufferedResponse(ctx, w, response.Response)
	case exchange.StreamingResponse:
		return writeStreamingSuccess(ctx, w, requestID, response.Response)
	}
	return transportpkg.DeliveryResult{Kind: transportpkg.DeliveryProviderStreamFailed, Err: canonical.InternalError("client response variant is invalid")}
}

func writeBufferedResponse(ctx context.Context, w http.ResponseWriter, response transportpkg.Response) transportpkg.DeliveryResult {
	if response.Body == nil {
		return transportpkg.DeliveryResult{Kind: transportpkg.DeliveryProviderStreamFailed, Err: canonical.InternalError("buffered client response is missing transport body")}
	}
	defer func() { _ = response.Body.Close() }()
	prefix, err := readFirstStreamChunk(response.Body)
	if err != nil {
		return classifyDeliveryFailure(ctx, response.Body, err, nil)
	}
	copyResponseHeaders(w, response.Header)
	w.WriteHeader(response.Status)
	if _, err := w.Write(prefix); err != nil {
		return classifyClientWriteFailure(ctx, err, nil)
	}
	_, err = io.Copy(w, response.Body)
	if err != nil {
		return classifyDeliveryFailure(ctx, response.Body, err, nil)
	}
	if terminalErr := responseBodyTerminalError(response.Body); terminalErr != nil {
		return classifyDeliveryFailure(ctx, response.Body, terminalErr, nil)
	}
	return transportpkg.DeliveryResult{Kind: transportpkg.DeliverySucceeded}
}

// swobu:lint ignore function-complexity because=streaming success encoding keeps transport branching local to one HTTP seam.
func writeStreamingSuccess(ctx context.Context, w http.ResponseWriter, requestID string, response transportpkg.Response) transportpkg.DeliveryResult {
	_ = requestID
	if response.Body == nil {
		return transportpkg.DeliveryResult{Kind: transportpkg.DeliveryProviderStreamFailed, Err: canonical.InternalError("streaming client response is missing transport body")}
	}

	closeDone := make(chan struct{})
	var once sync.Once
	closeBody := func() {
		once.Do(func() {
			// Keep the upstream response body tied to the live downstream socket so
			// disconnects can stop provider reads instead of silently stalling.
			_ = response.Body.Close()
			close(closeDone)
		})
	}
	defer closeBody()
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				closeBody()
			case <-closeDone:
			}
		}()
	}
	if closeNotifier, ok := w.(http.CloseNotifier); ok {
		if closeCh := closeNotifier.CloseNotify(); closeCh != nil {
			go func() {
				select {
				case <-closeCh:
					closeBody()
				case <-closeDone:
				}
			}()
		}
	}
	prefix, err := readFirstStreamChunk(response.Body)
	if err != nil {
		return classifyDeliveryFailure(ctx, response.Body, err, closeDone)
	}
	copyResponseHeaders(w, response.Header)
	w.WriteHeader(response.Status)
	if len(prefix) > 0 {
		if _, err := w.Write(prefix); err != nil {
			closeBody()
			return classifyClientWriteFailure(ctx, err, closeDone)
		}
	}
	// Flush each stream boundary so disconnects are observed on the live socket
	// instead of being deferred until handler teardown.
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	buf := make([]byte, 4096)
	for {
		n, readErr := response.Body.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				closeBody()
				return classifyClientWriteFailure(ctx, err, closeDone)
			}
			// Emit provider chunks promptly so the client connection can signal
			// cancellation before the upstream reader has to drain the whole body.
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			if terminalErr := responseBodyTerminalError(response.Body); terminalErr != nil {
				return classifyDeliveryFailure(ctx, response.Body, terminalErr, closeDone)
			}
			return transportpkg.DeliveryResult{Kind: transportpkg.DeliverySucceeded}
		}
		return classifyDeliveryFailure(ctx, response.Body, readErr, closeDone)
	}
}

func classifyClientWriteFailure(ctx context.Context, err error, closeDone <-chan struct{}) transportpkg.DeliveryResult {
	if streamDownstreamClosed(ctx, closeDone) {
		return transportpkg.DeliveryResult{Kind: transportpkg.DeliveryClientCancelled, Err: err}
	}
	return transportpkg.DeliveryResult{Kind: transportpkg.DeliveryClientWriteFailed, Err: err}
}

func classifyDeliveryFailure(ctx context.Context, body io.ReadCloser, err error, closeDone <-chan struct{}) transportpkg.DeliveryResult {
	if exchange.IsReplayCommitFailure(err) {
		return transportpkg.DeliveryResult{Kind: transportpkg.DeliveryReplayCommitFailed, Err: err}
	}
	if streamDownstreamClosed(ctx, closeDone) || errors.Is(err, context.Canceled) {
		return transportpkg.DeliveryResult{Kind: transportpkg.DeliveryClientCancelled, Err: err}
	}
	_ = body
	return transportpkg.DeliveryResult{Kind: transportpkg.DeliveryProviderStreamFailed, Err: err}
}

func responseBodyTerminalError(body io.ReadCloser) error {
	type terminalErrorSource interface{ TerminalError() error }
	if source, ok := body.(terminalErrorSource); ok {
		return source.TerminalError()
	}
	return nil
}

func readFirstStreamChunk(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, canonical.InternalError("streaming client response is missing transport body")
	}
	buf := make([]byte, 1)
	n, err := io.ReadAtLeast(body, buf, 1)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), buf[:n]...), nil
}

// Downstream disconnect is a terminal transport event, not a stream decode
// failure. Once the request context is canceled or the close signal fires, the
// stream writer should stop converting read/write fallout into internal errors.
func streamDownstreamClosed(ctx context.Context, closeDone <-chan struct{}) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	select {
	case <-closeDone:
		return true
	default:
		return false
	}
}

func copyResponseHeaders(w http.ResponseWriter, header http.Header) {
	for key, values := range header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
}
