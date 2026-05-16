package httpapi

import (
	"context"
	"errors"
	"io"

	httpcodec "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/httpcodec"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// frameSink is transport-specific frame emission (HTTP SSE, WebSocket, etc).
type frameSink interface {
	WriteFrame(frame []byte) error
	Flush() error
}

// drainEncodedFrames centralizes envelope source -> encoder -> sink flow.
func drainEncodedFrames(ctx context.Context, stream canonical.EventReader, encoder httpcodec.EnvelopeStreamEncoder, sink frameSink) error {
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			tail, tailErr := encoder.Finish()
			if tailErr != nil {
				return tailErr
			}
			for _, frame := range tail {
				if err := sink.WriteFrame(frame); err != nil {
					return err
				}
			}
			return sink.Flush()
		}
		if err != nil {
			return err
		}
		frames, err := encoder.EncodeEnvelopeEvent(event)
		if err != nil {
			return err
		}
		for _, frame := range frames {
			if err := sink.WriteFrame(frame); err != nil {
				return err
			}
		}
		if err := sink.Flush(); err != nil {
			return err
		}
	}
}
