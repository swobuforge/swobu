package responses

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseStreamEncoder) newStreamState() sse.EnvelopeStreamEncoder {
	return &sseEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ResponseStreamEncoder) EncodeResponseStream(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientByteStreamResult, error) {
	state := e.newStreamState()
	mediaType := "text/event-stream"
	body := wire.NewEncodedResponseBody(ctx, events, state.EncodeEnvelopeEvent)
	// Stage marks the carrier boundary for this streamed response leg; the
	// exchange graph owns path selection above the adapter edge.
	return wire.ClientByteStreamResult{Stream: carrier.ByteStream{MediaType: mediaType, Body: body}}, nil
}

func (e ResponseStreamEncoder) EncodeResponseMessages(_ context.Context, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientMessageResult, error) {
	if d.Framing != delivery.FramingWebSocket {
		return wire.ClientMessageResult{}, canonical.UnsupportedDelivery("responses message encoding requires websocket delivery")
	}
	stream := wire.NewEncodedResponseMessages(events, NewJSONEnvelopeStreamEncoder().EncodeEnvelopeEvent)
	return wire.ClientMessageResult{Response: carrier.MessageResponse{MediaType: "application/json", Messages: stream}}, nil
}
