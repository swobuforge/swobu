package carrier

import (
	"context"
	"net/http"
)

// MessageStream is a pull-based wire carrier whose item boundaries are
// protocol messages. Unlike ByteStream, one Next result has semantic transport
// meaning and must be delivered as one message by a message-oriented adapter.
type MessageStream interface {
	Next(context.Context) ([]byte, error)
	Close(context.Context) error
}

// MessageResponse carries one message-oriented client response without an
// interchangeable byte-reader representation.
type MessageResponse struct {
	Header    http.Header
	MediaType string
	Messages  MessageStream
}
