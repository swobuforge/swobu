package messages

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseStreamEncoder) newStreamState() sse.EnvelopeStreamEncoder {
	return &messagesEnvelopeStreamEncoder{
		adapter:                    sse.NewEnvelopeEventAdapter(),
		pendingWebSearchStarts:     map[string]sse.StreamEvent{},
		unresolvedWebSearchCallIDs: map[string]struct{}{},
		decisions:                  &compat.RecordingSink{},
	}
}

func (e ResponseStreamEncoder) EncodeResponseStream(ctx context.Context, request canonical.CanonicalRequest, events canonical.ResponseStream, _ delivery.Delivery) (wire.ClientByteStreamResult, error) {
	state := e.newStreamState()
	state.(*messagesEnvelopeStreamEncoder).request = request.Clone()
	completion, complete, fail := wire.NewResponseCompletion()
	encoder := messagesFingerprintingEncoder(request, state.EncodeEnvelopeEvent, complete, fail)
	body := wire.NewEncodedResponseBody(ctx, events, encoder, completion, fail)
	return wire.ClientByteStreamResult{
		Stream:            carrier.ByteStream{MediaType: "text/event-stream", Body: body},
		TerminalDecisions: state.(*messagesEnvelopeStreamEncoder).decisions,
		Completion:        completion,
	}, nil
}

func (e ResponseStreamEncoder) EncodeResponseMessages(context.Context, canonical.CanonicalRequest, canonical.ResponseStream, delivery.Delivery) (wire.ClientMessageResult, error) {
	return wire.ClientMessageResult{}, canonical.UnsupportedDelivery("messages does not support message-oriented client delivery")
}
