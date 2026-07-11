package responses

import (
	"context"
	"errors"
	"io"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func (ResponseStreamEncoder) newStreamState() sse.EnvelopeStreamEncoder {
	return &sseEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ResponseStreamEncoder) EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error) {
	state := e.newStreamState()
	framing := carrier.FramingSSE
	if d.Framing == delivery.FramingWebSocket {
		state = NewJSONEnvelopeStreamEncoder()
		framing = carrier.FramingWebSocket
	}
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = events.Close(context.Background()) }()
		defer func() { _ = pw.Close() }()
		for {
			ev, err := events.Next(context.Background())
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				_ = pw.CloseWithError(err)
				return
			}
			frames, err := state.EncodeEnvelopeEvent(ev)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			for _, frame := range frames {
				if _, err := pw.Write(frame); err != nil {
					_ = pw.CloseWithError(err)
					return
				}
			}
		}
	}()
	return carrier.WireStream{Family: protocolkind.Responses, Framing: framing, Frames: carrier.FrameReaderFromReadCloser(pr)}, nil
}
