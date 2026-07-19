package carrier

import (
	"io"
	"net/http"
)

// ByteStream is one raw byte stream on a wire leg. Body read boundaries carry
// no SSE, NDJSON, JSON, or WebSocket message semantics.
type ByteStream struct {
	Header    http.Header
	MediaType string
	Body      io.ReadCloser
}
