package exchange

import (
	"context"
	"io"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// responseHandoffStream replays the validated response-opening prefix consumed
// while Exchange still owns fallback. Once a provider has issued response
// identity, execution may have occurred and a streaming client can receive its
// protocol start; later failures belong to that selected stream.
type responseHandoffStream struct {
	prefix   []canonical.Event
	upstream canonical.ResponseStream
}

func prefetchResponseHandoff(ctx context.Context, upstream canonical.ResponseStream, clientStreaming bool) (canonical.ResponseStream, error) {
	held := make([]canonical.Event, 0, 2)
	for {
		event, err := upstream.Next(ctx)
		if err != nil {
			return nil, err
		}
		held = append(held, event)
		if responseHandoffEvent(event.Kind, clientStreaming) {
			return &responseHandoffStream{prefix: held, upstream: upstream}, nil
		}
		if len(held) > 2 {
			return nil, canonical.InternalError("response opening prefix exceeded handoff bound")
		}
	}
}

func responseHandoffEvent(kind canonical.EventKind, clientStreaming bool) bool {
	if clientStreaming {
		return kind != canonical.EventEnvelopeStart
	}
	return kind != canonical.EventEnvelopeStart && kind != canonical.EventResponseIdentity
}

func (s *responseHandoffStream) Next(ctx context.Context) (canonical.Event, error) {
	if len(s.prefix) > 0 {
		event := s.prefix[0]
		s.prefix = s.prefix[1:]
		return event, nil
	}
	if s.upstream == nil {
		return canonical.Event{}, io.EOF
	}
	return s.upstream.Next(ctx)
}

func (s *responseHandoffStream) Close(ctx context.Context) error {
	if s.upstream == nil {
		return nil
	}
	return s.upstream.Close(ctx)
}

var _ canonical.ResponseStream = (*responseHandoffStream)(nil)
