package exchange

import (
	"context"
	"sync"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// terminalCompatibilityStream commits compatibility evidence only after the provider
// stream has actually produced it. Evidence persistence is best effort and
// cannot alter stream semantics.
type terminalCompatibilityStream struct {
	upstream   canonical.ResponseStream
	sink       compat.Sink
	exchangeID string
	decisions  provider.DecisionSource
	once       sync.Once
}

func newTerminalCompatibilityStream(upstream canonical.ResponseStream, decisions provider.DecisionSource, sink compat.Sink, exchangeID string) canonical.ResponseStream {
	return &terminalCompatibilityStream{upstream: upstream, decisions: decisions, sink: sink, exchangeID: exchangeID}
}

func (s *terminalCompatibilityStream) Next(ctx context.Context) (canonical.Event, error) {
	event, err := s.upstream.Next(ctx)
	if err != nil || isResponseTerminalEvent(event) {
		s.flush(ctx)
	}
	return event, err
}

func (s *terminalCompatibilityStream) Close(ctx context.Context) error {
	err := s.upstream.Close(ctx)
	s.flush(ctx)
	return err
}

func (s *terminalCompatibilityStream) flush(ctx context.Context) {
	s.once.Do(func() {
		if s.decisions != nil {
			commitDecisionsBestEffort(ctx, s.sink, s.exchangeID, s.decisions.Decisions())
		}
	})
}

func isResponseTerminalEvent(event canonical.Event) bool {
	if event.Kind != canonical.EventEnvelopeEnd {
		return false
	}
	payload, ok := event.Payload.(canonical.EnvelopeEndPayload)
	return ok && payload.Kind == canonical.EnvResponse
}

var _ canonical.ResponseStream = (*terminalCompatibilityStream)(nil)
