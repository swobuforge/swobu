package completions

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
	return &completionsEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ResponseStreamEncoder) EncodeResponseStream(events canonical.EventReader, _ delivery.Delivery) (effect.Result[carrier.WireStream], error) {
	state := e.newStreamState()
	return openaicompat.EncodeEnvelopeStream(events, state, protocolkind.Completions)
}
