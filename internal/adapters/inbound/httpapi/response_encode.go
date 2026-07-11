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

func writeSuccessResponse(ctx context.Context, w http.ResponseWriter, requestID string, family canonical.ClientFamily, out exchange.RequestOutput) error {
	response := out.Response.Transport
	_ = family
	if !isStreamingTransportResponse(response) {
		if response.Body == nil {
			return canonical.InternalError("buffered client response is missing transport body")
		}
		return writeBufferedResponse(w, response)
	}
	if response.Body == nil {
		return canonical.InternalError("streaming client response is missing transport body")
	}
	return writeStreamingSuccess(ctx, w, requestID, family, response)
}

func isStreamingTransportResponse(response transportpkg.TransportResponse) bool {
	return response.Header.Get("Content-Type") == "text/event-stream"
}

func writeBufferedResponse(w http.ResponseWriter, response transportpkg.TransportResponse) error {
	copyResponseHeaders(w, response.Header)
	w.WriteHeader(response.Status)
	defer func() { _ = response.Body.Close() }()
	_, err := io.Copy(w, response.Body)
	return err
}

func writeStreamingSuccess(ctx context.Context, w http.ResponseWriter, requestID string, family canonical.ClientFamily, response transportpkg.TransportResponse) error {
	_ = requestID
	_ = family

	closeDone := make(chan struct{})
	writerDone := make(chan struct{})
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
	defer close(writerDone)
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
		return canonical.InternalError("stream decoding failed")
	}
	copyResponseHeaders(w, response.Header)
	w.WriteHeader(response.Status)
	if len(prefix) > 0 {
		if _, err := w.Write(prefix); err != nil {
			return canonical.InternalError("stream decoding failed")
		}
	}
	// Flush each stream boundary so disconnects are observed on the live socket
	// instead of being deferred until handler teardown.
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	type streamChunk struct {
		bytes []byte
		err   error
	}
	chunks := make(chan streamChunk, 1)
	go func() {
		defer close(chunks)
		buf := make([]byte, 4096)
		for {
			n, readErr := response.Body.Read(buf)
			chunk := streamChunk{}
			if n > 0 {
				chunk.bytes = append([]byte(nil), buf[:n]...)
			}
			if readErr != nil {
				chunk.err = readErr
			}
			select {
			case chunks <- chunk:
			case <-writerDone:
				return
			}
			if readErr != nil {
				return
			}
		}
	}()
	for chunk := range chunks {
		if len(chunk.bytes) > 0 {
			if _, err := w.Write(chunk.bytes); err != nil {
				return canonical.InternalError("stream decoding failed")
			}
			// Emit provider chunks promptly so the client connection can signal
			// cancellation before the upstream reader has to drain the whole body.
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		if chunk.err == nil {
			continue
		}
		if errors.Is(chunk.err, io.EOF) {
			return nil
		}
		return canonical.InternalError("stream decoding failed")
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

func copyResponseHeaders(w http.ResponseWriter, header http.Header) {
	for key, values := range header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
}
