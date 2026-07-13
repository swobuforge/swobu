package completions

import (
	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	openaicompat "github.com/swobuforge/swobu/internal/adapters/wire/shared/openaicompat"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

func (ResponseStreamEncoder) newStreamState() sse.EnvelopeStreamEncoder {
	return &completionsEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ResponseStreamEncoder) EncodeResponseStream(events canonical.EventReader, _ delivery.Delivery) (exchange.Result[carrier.WireStream], error) {
	state := e.newStreamState()
	return openaicompat.EncodeEnvelopeStream(events, state, protocolkind.Completions)
}
