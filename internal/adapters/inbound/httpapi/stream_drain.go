package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type streamDrainStats struct {
	EventCount  int
	FrameCount  int
	FrameBytes  int
	FrameSHA256 string
}

// drainEncodedFrames centralizes envelope source -> encoder -> sink flow.
func drainEncodedFrames(ctx context.Context, stream canonical.EventReader, encoder httpcodec.EnvelopeStreamEncoder, sink frameSink) error {
	_, err := drainEncodedFramesWithStats(ctx, stream, encoder, sink)
	return err
}

func drainEncodedFramesWithStats(ctx context.Context, stream canonical.EventReader, encoder httpcodec.EnvelopeStreamEncoder, sink frameSink) (streamDrainStats, error) {
	stats := streamDrainStats{}
	hash := sha256.New()
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			tail, tailErr := encoder.Finish()
			if tailErr != nil {
				return streamDrainStats{}, tailErr
			}
			for _, frame := range tail {
				if err := sink.WriteFrame(frame); err != nil {
					return streamDrainStats{}, err
				}
				_, _ = hash.Write(frame)
				stats.FrameCount++
				stats.FrameBytes += len(frame)
			}
			stats.FrameSHA256 = hex.EncodeToString(hash.Sum(nil))
			return stats, sink.Flush()
		}
		if err != nil {
			return streamDrainStats{}, err
		}
		stats.EventCount++
		frames, err := encoder.EncodeEnvelopeEvent(event)
		if err != nil {
			return streamDrainStats{}, err
		}
		for _, frame := range frames {
			if err := sink.WriteFrame(frame); err != nil {
				return streamDrainStats{}, err
			}
			_, _ = hash.Write(frame)
			stats.FrameCount++
			stats.FrameBytes += len(frame)
		}
		if err := sink.Flush(); err != nil {
			return streamDrainStats{}, err
		}
	}
}
