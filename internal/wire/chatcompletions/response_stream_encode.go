package chatcompletions

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseStreamEncoder) newStreamState(includeUsageFrame bool) sse.EnvelopeStreamEncoder {
	return &chatCompletionsEnvelopeStreamEncoder{
		adapter:               sse.NewEnvelopeEventAdapter(),
		pendingWebSearchCalls: map[string]uint32{},
		changes:               []compat.Change{},
		includeUsageFrame:     includeUsageFrame,
	}
}

func (e ResponseStreamEncoder) EncodeResponseStream(ctx context.Context, _ canonical.CanonicalRequest, events canonical.ResponseStream, responseDelivery delivery.Delivery) (wire.ClientByteStreamResult, error) {
	state := e.newStreamState(responseDelivery.IncludeUsageFrame)
	streamState := state.(*chatCompletionsEnvelopeStreamEncoder)
	completion, complete, fail := wire.NewResponseCompletion()
	encoder := chatCompletionsFingerprintingEncoder(state.EncodeEnvelopeEvent, func(fingerprint *historyfingerprint.Response) {
		complete(fingerprint, streamState.Changes())
	}, fail)
	body := wire.NewEncodedResponseBody(ctx, events, encoder, completion, fail)
	return wire.ClientByteStreamResult{
		Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: body}, Completion: completion,
	}, nil
}

func (e ResponseStreamEncoder) EncodeResponseMessages(context.Context, canonical.CanonicalRequest, canonical.ResponseStream, delivery.Delivery) (wire.ClientMessageResult, error) {
	return wire.ClientMessageResult{}, canonical.ClientUnsupportedDelivery(
		"Chat Completions does not support message-oriented client delivery",
		"Use buffered or SSE HTTP delivery and retry",
	)
}
