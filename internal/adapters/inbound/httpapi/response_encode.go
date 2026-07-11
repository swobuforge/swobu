package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

func writeSuccessResponse(w http.ResponseWriter, requestID string, family canonical.ClientFamily, out exchange.RequestOutput) error {
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
	return writeStreamingSuccess(w, requestID, family, response)
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

func writeStreamingSuccess(w http.ResponseWriter, requestID string, family canonical.ClientFamily, response transportpkg.TransportResponse) error {
	_ = requestID
	_ = family

	prefix, err := readFirstStreamChunk(response.Body)
	if err != nil {
		return canonical.InternalError("stream decoding failed")
	}
	copyResponseHeaders(w, response.Header)
	w.WriteHeader(response.Status)
	defer func() { _ = response.Body.Close() }()
	if len(prefix) > 0 {
		if _, err := w.Write(prefix); err != nil {
			return canonical.InternalError("stream decoding failed")
		}
	}
	if _, err := io.Copy(w, response.Body); err != nil && !errors.Is(err, io.EOF) {
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
