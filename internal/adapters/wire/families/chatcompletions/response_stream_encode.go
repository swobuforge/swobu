package chatcompletions

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
	return &chatCompletionsEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ResponseStreamEncoder) EncodeResponseStream(events canonical.EventReader, _ delivery.Delivery) (carrier.WireStream, error) {
	state := e.newStreamState()
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
	return carrier.WireStream{Stage: carrier.StageClientResponseOut, Family: protocolkind.ChatCompletions, Framing: carrier.FramingSSE, Frames: carrier.FrameReaderFromReadCloser(pr)}, nil
}
