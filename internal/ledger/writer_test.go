package ledger

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
)

type writerStore struct {
	mu         sync.Mutex
	accepted   int
	failures   int
	closed     bool
	maintained int
	entered    chan struct{}
	release    chan struct{}
}

func (s *writerStore) WriteBatch(_ context.Context, exchanges []executionevidence.CompletedExchange) error {
	if s.entered != nil {
		select {
		case s.entered <- struct{}{}:
		default:
		}
	}
	if s.release != nil {
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures > 0 {
		s.failures--
		return errors.New("injected store failure")
	}
	s.accepted += len(exchanges)
	return nil
}

func TestWriterCloseDrainsBatchAlreadyOwnedByWorker(t *testing.T) {
	store := &writerStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	w, err := NewWriter(store, WriterConfig{QueueCapacity: 1, BatchSize: 1, DrainBudget: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Offer(executionevidence.CompletedExchange{}) {
		t.Fatal("offer dropped")
	}
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not own batch")
	}
	closed := make(chan error, 1)
	go func() { closed <- w.Close() }()
	close(store.release)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	accepted := store.accepted
	store.mu.Unlock()
	if accepted != 1 {
		t.Fatalf("accepted=%d, want current batch drained", accepted)
	}
}

func (s *writerStore) ListRecentSummaries(context.Context, ExchangeFilter, Cursor, int) (ExchangeSummaryPage, error) {
	return ExchangeSummaryPage{}, nil
}
func (s *writerStore) GetExchange(context.Context, execution.ExchangeID) (executionevidence.CompletedExchange, bool, error) {
	return executionevidence.CompletedExchange{}, false, nil
}
func (s *writerStore) Maintain(context.Context, time.Time) error {
	s.mu.Lock()
	s.maintained++
	s.mu.Unlock()
	return nil
}
func (s *writerStore) Close() error { s.mu.Lock(); s.closed = true; s.mu.Unlock(); return nil }

func TestWriterOffersNonBlockingAndBoundsQueue(t *testing.T) {
	store := &writerStore{}
	w, err := NewWriter(store, WriterConfig{QueueCapacity: 1, BatchSize: 100, BatchWait: time.Hour, DrainBudget: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	first := executionevidence.CompletedExchange{}
	if !w.Offer(first) {
		t.Fatal("first offer dropped")
	}
	if w.Offer(first) {
		t.Fatal("full queue accepted second offer")
	}
	health := w.Health()
	if health.QueueCapacity != 1 || health.DroppedExchangesTotal != 1 || health.State != StateDegraded {
		t.Fatalf("health=%#v", health)
	}
}

func TestWriterRejectsOffersAfterClose(t *testing.T) {
	store := &writerStore{}
	w, err := NewWriter(store, WriterConfig{QueueCapacity: 1, BatchWait: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if w.Offer(executionevidence.CompletedExchange{}) {
		t.Fatal("offer accepted after close")
	}
}

func TestWriterRetriesStoreFailureAndCommitsBatch(t *testing.T) {
	store := &writerStore{failures: 2}
	w, err := NewWriter(store, WriterConfig{QueueCapacity: 4, BatchSize: 1, BatchWait: time.Millisecond, DrainBudget: time.Second, Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Offer(executionevidence.CompletedExchange{}) {
		t.Fatal("offer dropped")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		store.mu.Lock()
		accepted := store.accepted
		store.mu.Unlock()
		if accepted == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	store.mu.Lock()
	accepted := store.accepted
	store.mu.Unlock()
	if accepted != 1 {
		t.Fatalf("accepted=%d after retries", accepted)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWriterRunsMaintenanceAfterCommitThreshold(t *testing.T) {
	store := &writerStore{}
	w, err := NewWriter(store, WriterConfig{QueueCapacity: 2048, BatchSize: 256, BatchWait: time.Millisecond, DrainBudget: time.Second, Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1024; i++ {
		if !w.Offer(executionevidence.CompletedExchange{}) {
			t.Fatal("offer dropped")
		}
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		store.mu.Lock()
		n := store.maintained
		store.mu.Unlock()
		if n > 0 {
			_ = w.Close()
			return
		}
		time.Sleep(time.Millisecond)
	}
	_ = w.Close()
	t.Fatal("maintenance was not invoked")
}

func TestWriterCloseIsBoundedWhenStoreStaysDown(t *testing.T) {
	store := &writerStore{failures: 1_000_000}
	w, err := NewWriter(store, WriterConfig{QueueCapacity: 1, BatchSize: 1, BatchWait: time.Millisecond, DrainBudget: 20 * time.Millisecond, Sleep: func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Offer(executionevidence.CompletedExchange{}) {
		t.Fatal("offer dropped")
	}
	started := time.Now()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("writer close exceeded bound: %s", elapsed)
	}
	if health := w.Health(); health.LastError != ErrorShutdownTimeout {
		t.Fatalf("final health=%#v", health)
	}
}
