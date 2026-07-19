package transport

import (
	"context"
	"io"
	"net/http"
)

// MessageStream preserves message-oriented transport boundaries.
type MessageStream interface {
	Next(context.Context) ([]byte, error)
	Close(context.Context) error
}

// TransportRequest is one HTTP-level request carrier.
type TransportRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   io.ReadCloser
}

// Response is one HTTP-level response carrier.
type Response struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
}

// MessageResponse is the message-oriented counterpart to Response. It cannot
// be consumed as an arbitrary byte stream.
type MessageResponse struct {
	Status   int
	Header   http.Header
	Messages MessageStream
}
