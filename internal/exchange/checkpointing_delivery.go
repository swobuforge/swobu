package exchange

import (
	"context"
	"io"
	"sync"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
)

type checkpointingReadCloser struct {
	ctx        context.Context
	inner      io.ReadCloser
	committer  *checkpointCommitter
	completion *wire.ResponseCompletion
	sink       compat.Sink
	exchangeID string
	decisions  provider.DecisionSource
	once       sync.Once
}

func (b *checkpointingReadCloser) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	if commitErr := b.committer.commitIfReady(b.ctx, b.completion); commitErr != nil {
		return 0, commitErr
	}
	if err != nil || b.completion.Snapshot().State != wire.CompletionPending {
		b.commitTerminalDecisions()
	}
	return n, err
}

func (b *checkpointingReadCloser) Close() error {
	err := b.inner.Close()
	b.commitTerminalDecisions()
	return err
}

func (b *checkpointingReadCloser) commitTerminalDecisions() {
	b.once.Do(func() {
		if b.decisions != nil {
			commitDecisionsBestEffort(b.ctx, b.sink, b.exchangeID, b.decisions.Decisions())
		}
	})
}

type checkpointingMessageStream struct {
	inner      carrier.MessageStream
	committer  *checkpointCommitter
	completion *wire.ResponseCompletion
	once       sync.Once
}

func (s *checkpointingMessageStream) Next(ctx context.Context) ([]byte, error) {
	message, err := s.inner.Next(ctx)
	if commitErr := s.committer.commitIfReady(ctx, s.completion); commitErr != nil {
		return nil, commitErr
	}
	return message, err
}

func (s *checkpointingMessageStream) Close(ctx context.Context) error {
	var err error
	s.once.Do(func() { err = s.inner.Close(ctx) })
	return err
}
