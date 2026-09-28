package exchange

import (
	"context"
	"errors"
	"sync"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/execution"
)

// providerEvidenceStream observes the decoded canonical provider stream before
// checkpointing and client encoding.
type providerEvidenceStream struct {
	upstream    canonical.ResponseStream
	settle      func(execution.Outcome, canonical.TokenUsage)
	mu          sync.Mutex
	settled     bool
	usage       canonical.TokenUsage
	settledDone chan struct{}
}

func newProviderEvidenceStream(upstream canonical.ResponseStream, settle func(execution.Outcome, canonical.TokenUsage)) canonical.ResponseStream {
	return &providerEvidenceStream{upstream: upstream, settle: settle, usage: canonical.NewUnknownTokenUsage(), settledDone: make(chan struct{})}
}

func (s *providerEvidenceStream) Next(ctx context.Context) (canonical.Event, error) {
	event, err := s.upstream.Next(ctx)
	if err != nil {
		outcome := execution.Failed
		if errors.Is(err, context.Canceled) {
			outcome = execution.Cancelled
		}
		s.settleTerminal(outcome, canonical.NewUnknownTokenUsage())
		return canonical.Event{}, err
	}
	s.mu.Lock()
	if !s.settled {
		if event.Kind == canonical.EventUsage {
			if payload, ok := event.Payload.(canonical.UsagePayload); ok {
				s.usage = payload.Usage
			}
		}
		if status, terminal := responseTerminalStatus(event); terminal {
			s.settled = true
			outcome := execution.Failed
			usage := canonical.NewUnknownTokenUsage()
			if status == canonical.EnvelopeStatusCompleted {
				outcome = execution.Succeeded
				usage = s.usage
			}
			s.mu.Unlock()
			defer close(s.settledDone)
			s.settle(outcome, usage)
			return event, nil
		}
	}
	s.mu.Unlock()
	return event, nil
}

func (s *providerEvidenceStream) Close(ctx context.Context) error {
	s.settleTerminal(execution.Cancelled, canonical.NewUnknownTokenUsage())
	return s.upstream.Close(ctx)
}

// settleTerminal elects the first observed terminal without holding the lock
// across a callback, which may synchronously append to the evidence store.
func (s *providerEvidenceStream) settleTerminal(outcome execution.Outcome, usage canonical.TokenUsage) {
	s.mu.Lock()
	if s.settled {
		s.mu.Unlock()
		<-s.settledDone
		return
	}
	s.settled = true
	s.mu.Unlock()
	defer close(s.settledDone)
	s.settle(outcome, usage)
}
