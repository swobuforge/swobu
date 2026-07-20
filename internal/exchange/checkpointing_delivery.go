package exchange

import (
	"context"
	"io"
	"sync"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/wire"
)

type checkpointingReadCloser struct {
	ctx        context.Context
	inner      io.ReadCloser
	committer  *checkpointCommitter
	completion *wire.ResponseCompletion
}

func (b *checkpointingReadCloser) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	if commitErr := b.committer.commitIfReady(b.ctx, b.completion); commitErr != nil {
		return 0, commitErr
	}
	return n, err
}

func (b *checkpointingReadCloser) Close() error { return b.inner.Close() }

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
