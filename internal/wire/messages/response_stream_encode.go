package messages

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
	openaicompat "github.com/swobuforge/swobu/internal/wire/openai"
)

func (ResponseStreamEncoder) newStreamState() sse.EnvelopeStreamEncoder {
	return &messagesEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ResponseStreamEncoder) EncodeResponseStream(events canonical.EventReader, _ delivery.Delivery) (effect.Result[carrier.CarrierStream], error) {
	state := e.newStreamState()
	return openaicompat.EncodeEnvelopeStream(events, state, protocolkind.Messages)
}
