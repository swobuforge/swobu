package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// frameSink is transport-specific frame emission (HTTP SSE, WebSocket, etc).
type frameSink interface {
	WriteFrame(frame []byte) error
	Flush() error
}

type streamDrainCounters struct {
	EventCount  int
	FrameCount  int
	FrameBytes  int
	FrameSHA256 string
}

// drainEncodedFrames is the HTTP-edge streaming pump used by SSE/WebSocket
// handlers: read canonical envelope events, encode transport frames, write and
// flush frames to the client sink.
//
// Keeping this loop in one place prevents drift between streaming surfaces and
// preserves one shutdown/error behavior for all transport sinks.
func drainEncodedFrames(ctx context.Context, stream canonical.EventReader, encoder sse.EnvelopeStreamEncoder, sink frameSink) error {
	_, err := drainEncodedFramesWithStats(ctx, stream, encoder, sink)
	return err
}

// drainEncodedFramesWithStats performs the same pump as drainEncodedFrames but
// also returns deterministic counters used by tests to assert stream shape.
func drainEncodedFramesWithStats(ctx context.Context, stream canonical.EventReader, encoder sse.EnvelopeStreamEncoder, sink frameSink) (streamDrainCounters, error) {
	stats := streamDrainCounters{}
	hash := sha256.New()
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			// EOF from canonical stream is the expected terminal path.
			// Encoder.Finish emits required trailer frames (for example final
			// SSE control frames) so transports see a complete protocol stream.
			tail, tailErr := encoder.Finish()
			if tailErr != nil {
				return streamDrainCounters{}, tailErr
			}
			for _, frame := range tail {
				if err := sink.WriteFrame(frame); err != nil {
					return streamDrainCounters{}, err
				}
				_, _ = hash.Write(frame)
				stats.FrameCount++
				stats.FrameBytes += len(frame)
			}
			stats.FrameSHA256 = hex.EncodeToString(hash.Sum(nil))
			return stats, sink.Flush()
		}
		if err != nil {
			// Non-EOF failures are surfaced directly; caller decides whether to
			// map to transport close/error semantics.
			return streamDrainCounters{}, err
		}
		stats.EventCount++
		frames, err := encoder.EncodeEnvelopeEvent(event)
		if err != nil {
			return streamDrainCounters{}, err
		}
		for _, frame := range frames {
			if err := sink.WriteFrame(frame); err != nil {
				return streamDrainCounters{}, err
			}
			_, _ = hash.Write(frame)
			stats.FrameCount++
			stats.FrameBytes += len(frame)
		}
		if err := sink.Flush(); err != nil {
			return streamDrainCounters{}, err
		}
	}
}
