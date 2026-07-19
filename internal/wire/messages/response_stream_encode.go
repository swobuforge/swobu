package messages

import (
	"context"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

func (ResponseStreamEncoder) newStreamState() sse.EnvelopeStreamEncoder {
	return &messagesEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ResponseStreamEncoder) EncodeResponseStream(ctx context.Context, events canonical.ResponseStream, _ delivery.Delivery) (wire.ClientByteStreamResult, error) {
	state := e.newStreamState()
	return openaiwire.EncodeEnvelopeStream(ctx, events, state)
}

func (e ResponseStreamEncoder) EncodeResponseMessages(context.Context, canonical.ResponseStream, delivery.Delivery) (wire.ClientMessageResult, error) {
	return wire.ClientMessageResult{}, canonical.UnsupportedDelivery("messages does not support message-oriented client delivery")
}
