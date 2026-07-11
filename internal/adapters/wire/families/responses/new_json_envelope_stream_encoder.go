package responses

import (
	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// NewJSONEnvelopeStreamEncoder bridges canonical envelope events to the
// Responses wire event stream encoder.
func NewJSONEnvelopeStreamEncoder() sse.EnvelopeStreamEncoder {
	wire := NewResponseStreamWireEncoder()
	return &jsonEnvelopeStreamEncoder{
		wire:    &wire,
		adapter: sse.NewEnvelopeEventAdapter(),
	}
}

type jsonEnvelopeStreamEncoder struct {
	wire    *ResponseStreamWireEncoder
	adapter *sse.EnvelopeEventAdapter
}

func (e *jsonEnvelopeStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
	streamEvents := e.adapter.Translate(event)
	frames := make([][]byte, 0, len(streamEvents))
	for _, streamEvent := range streamEvents {
		emitted, err := e.wire.Encode(streamEvent)
		if err != nil {
			return nil, err
		}
		frames = append(frames, emitted...)
	}
	return frames, nil
}

func (e *jsonEnvelopeStreamEncoder) Finish() ([][]byte, error) {
	if e == nil || e.wire == nil {
		return nil, nil
	}
	return e.wire.Finish()
}
