package chatcompletions

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseStreamEncoder) newStreamState() sse.EnvelopeStreamEncoder {
	return &chatCompletionsEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ResponseStreamEncoder) EncodeResponseStream(ctx context.Context, _ canonical.CanonicalRequest, events canonical.ResponseStream, _ delivery.Delivery) (wire.ClientByteStreamResult, error) {
	state := e.newStreamState()
	completion, complete, fail := wire.NewResponseCompletion()
	encoder := chatCompletionsFingerprintingEncoder(state.EncodeEnvelopeEvent, complete, fail)
	body := wire.NewEncodedResponseBody(ctx, events, encoder, completion, fail)
	return wire.ClientByteStreamResult{
		Stream:     carrier.ByteStream{MediaType: "text/event-stream", Body: body},
		Completion: completion,
	}, nil
}

func (e ResponseStreamEncoder) EncodeResponseMessages(context.Context, canonical.CanonicalRequest, canonical.ResponseStream, delivery.Delivery) (wire.ClientMessageResult, error) {
	return wire.ClientMessageResult{}, canonical.UnsupportedDelivery("chat completions does not support message-oriented client delivery")
}
