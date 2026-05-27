package httpedge

import (
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/platform/httpcontent"
)

func JoinBaseURLAndPath(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func DecodeHTTPResponseContentEncoding(resp *http.Response) (*http.Response, error) {
	contentEncoding := strings.TrimSpace(resp.Header.Get("Content-Encoding")) // swobu:io-string source=boundary
	if contentEncoding == "" {
		return resp, nil
	}
	decodedBody, err := httpcontent.DecodeStream(contentEncoding, resp.Body)
	if err != nil {
		return nil, err
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
