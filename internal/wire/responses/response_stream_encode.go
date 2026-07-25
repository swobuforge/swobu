package responses

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseStreamEncoder) newStreamState(request canonical.CanonicalRequest) sse.EnvelopeStreamEncoder {
	return &sseEnvelopeStreamEncoder{wire: NewResponseStreamWireEncoder(request), adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ResponseStreamEncoder) EncodeResponseStream(ctx context.Context, request canonical.CanonicalRequest, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientByteStreamResult, error) {
	state := e.newStreamState(request)
	completion, complete, fail := wire.NewResponseCompletion()
	mediaType := "text/event-stream"
	encoder := responsesFingerprintingEncoder(request, state.EncodeEnvelopeEvent, complete, fail)
	body := wire.NewEncodedResponseBody(ctx, events, encoder, completion, fail)
	// Stage marks the carrier boundary for this streamed response leg; the
	// exchange graph owns path selection above the adapter edge.
	return wire.ClientByteStreamResult{Stream: carrier.ByteStream{MediaType: mediaType, Body: body}, Completion: completion}, nil
}

func (e ResponseStreamEncoder) EncodeResponseMessages(ctx context.Context, request canonical.CanonicalRequest, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientMessageResult, error) {
	if d.Framing != delivery.FramingWebSocket {
		return wire.ClientMessageResult{}, canonical.InternalError("Responses message encoder requires websocket delivery")
	}
	completion, complete, fail := wire.NewResponseCompletion()
	encoder := responsesFingerprintingEncoder(request, NewJSONEnvelopeStreamEncoder(request).EncodeEnvelopeEvent, complete, fail)
	stream := wire.NewEncodedResponseMessages(events, encoder, completion, fail)
	return wire.ClientMessageResult{Response: carrier.MessageResponse{MediaType: "application/json", Messages: stream}, Completion: completion}, nil
}
