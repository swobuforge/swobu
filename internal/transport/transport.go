package transport

import (
	"io"
	"net/http"
)

// TransportRequest is one HTTP-level request carrier.
type TransportRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   io.ReadCloser
}

// TransportResponse is one HTTP-level response carrier.
type TransportResponse struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
}
