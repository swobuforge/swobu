package openaicompat

import (
	"context"
	"errors"
	"io"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

// EncodeEnvelopeStream pipes canonical events through an SSE envelope encoder.
//
// This helper captures the mechanical event-loop pattern shared by all
// OpenAI-family streaming adapters. Families inject their encoder state and
// wire-stream metadata; the helper owns only the pipe-forwarding mechanics.
func EncodeEnvelopeStream(
	events canonical.EventReader,
	encoder sse.EnvelopeStreamEncoder,
	family protocolkind.ProtocolKind,
) (exchange.Result[carrier.WireStream], error) {
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
			frames, err := encoder.EncodeEnvelopeEvent(ev)
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
	return exchange.NewResult(carrier.WireStream{Family: family, Framing: carrier.FramingSSE, Frames: carrier.FrameReaderFromReadCloser(pr)}), nil
}
