package core

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

type blockingReadCloser struct {
	closed chan struct{}
}

func (b *blockingReadCloser) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *blockingReadCloser) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

func TestSSEReaderCancellationClosesBlockingProviderBody(t *testing.T) {
	body := &blockingReadCloser{closed: make(chan struct{})}
	reader := NewSSEReader(body)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := reader.Next(ctx)
		result <- err
	}()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Next error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("provider stream read ignored invocation cancellation")
	}
}
