package httpedge

import (
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/platform/httpcontent"
)

const maxUnexpectedStreamingEvidence = 64 << 10

func JoinBaseURLAndPath(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

// IsEventStreamContentType reports whether raw is an exact SSE media type.
// Parameters and case differences are accepted; malformed values and
// lookalikes are not.
func IsEventStreamContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(raw)) // swobu:io-string source=boundary
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

func DecodeHTTPResponseContentEncoding(resp *http.Response) (*http.Response, error) {
	contentEncoding := strings.TrimSpace(resp.Header.Get("Content-Encoding")) // swobu:io-string source=boundary
	if contentEncoding == "" {
		return resp, nil
	}
	decodedBody, err := httpcontent.DecodeStream(contentEncoding, resp.Body)
	if err != nil {
		return resp, err
	}
	resp.Body = decodedBody
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	return resp, nil
}

func ReadBackendHTTPError(resp *http.Response, backendRef string) canonical.BackendError {
	raw, _ := io.ReadAll(resp.Body)
	return canonical.NewBackendError(
		backendRef,
		resp.StatusCode,
		strings.TrimSpace(string(raw)), // swobu:io-string source=boundary
		strings.TrimSpace(resp.Header.Get("Retry-After")), // swobu:io-string source=boundary
	)
}

// ReadUnexpectedStreamingResponse closes a successful response that violated
// an SSE-only provider delivery contract and preserves bounded backend evidence.
func ReadUnexpectedStreamingResponse(resp *http.Response, backendRef string) canonical.BackendError {
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxUnexpectedStreamingEvidence+1))
	if len(raw) > maxUnexpectedStreamingEvidence {
		raw = raw[:maxUnexpectedStreamingEvidence]
	}
	return canonical.NewBackendError(
		backendRef,
		http.StatusBadGateway,
		strings.TrimSpace(string(raw)), // swobu:io-string source=boundary
		strings.TrimSpace(resp.Header.Get("Retry-After")), // swobu:io-string source=boundary
	)
}
