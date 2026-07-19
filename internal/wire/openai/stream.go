package openai

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

// EncodeEnvelopeStream exposes a pull-based SSE body over canonical events.
//
// This helper captures the mechanical event-loop pattern shared by all
// OpenAI-family streaming adapters. Families inject their encoder state and
// wire-stream metadata; the delivery owner drives reads and closure.
func EncodeEnvelopeStream(
	ctx context.Context,
	events canonical.ResponseStream,
	encoder sse.EnvelopeStreamEncoder,
) (wire.ClientByteStreamResult, error) {
	body := wire.NewEncodedResponseBody(ctx, events, encoder.EncodeEnvelopeEvent)
	return wire.ClientByteStreamResult{Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: body}}, nil
}
