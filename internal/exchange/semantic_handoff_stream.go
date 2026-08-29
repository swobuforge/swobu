package exchange

import (
	"context"
	"io"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// semanticHandoffStream replays the bounded provider-lifecycle prefix consumed
// while Exchange still owns fallback. It never buffers substantive output.
type semanticHandoffStream struct {
	prefix   []canonical.Event
	upstream canonical.ResponseStream
}

func prefetchSemanticHandoff(ctx context.Context, upstream canonical.ResponseStream) (canonical.ResponseStream, error) {
	held := make([]canonical.Event, 0, 3)
	for {
		event, err := upstream.Next(ctx)
		if err != nil {
			return nil, err
		}
		held = append(held, event)
		if semanticHandoffEvent(event.Kind) {
			return &semanticHandoffStream{prefix: held, upstream: upstream}, nil
		}
		if len(held) > 3 {
			return nil, canonical.InternalError("response lifecycle prefix exceeded semantic handoff bound")
		}
	}
}

func semanticHandoffEvent(kind canonical.EventKind) bool {
	return kind != canonical.EventEnvelopeStart && kind != canonical.EventResponseIdentity
}

func (s *semanticHandoffStream) Next(ctx context.Context) (canonical.Event, error) {
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

func (s *semanticHandoffStream) Close(ctx context.Context) error {
	if s.upstream == nil {
		return nil
	}
	return s.upstream.Close(ctx)
}

var _ canonical.ResponseStream = (*semanticHandoffStream)(nil)
