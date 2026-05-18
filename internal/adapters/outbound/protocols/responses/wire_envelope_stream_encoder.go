package responses

import (
	httpcodec "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/httpcodec"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// NewWireEnvelopeStreamEncoder bridges canonical envelope events to the
// Responses wire event stream encoder.
func NewWireEnvelopeStreamEncoder() httpcodec.EnvelopeStreamEncoder {
	wire := NewResponsesClientStreamEncoderWire()
	return &wireEnvelopeStreamEncoder{
		wire:    &wire,
		adapter: httpcodec.NewEnvelopeEventAdapter(),
	}
}

type wireEnvelopeStreamEncoder struct {
	wire    *ResponsesClientStreamEncoderWire
	adapter *httpcodec.EnvelopeEventAdapter
}

func (e *wireEnvelopeStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
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

func (e *wireEnvelopeStreamEncoder) Finish() ([][]byte, error) {
	if e == nil || e.wire == nil {
		return nil, nil
	}
	return e.wire.Finish()
}
