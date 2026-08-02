package carrier

import (
	"io"
	"net/http"
)

// TransportRequest carries one HTTP-level client request into exchange.
type TransportRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

// Response carries one HTTP-level client response to the inbound adapter.
type Response struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
}

// MessageTransportResponse is the message-oriented counterpart to Response.
// Its items retain protocol message boundaries until the inbound adapter writes
// them to a message-oriented transport.
type MessageTransportResponse struct {
	Status   int
	Header   http.Header
	Messages MessageStream
}
