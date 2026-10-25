package ledger

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/routing"
)

type writerStore struct {
	mu          sync.Mutex
	accepted    int
	mcpAccepted int
	failures    int
	mcpFailures int
	closed      bool
	maintained  int
	entered     chan struct{}
	release     chan struct{}
}

func (s *writerStore) WriteMCPOperations(ctx context.Context, operations []executionevidence.CompletedMCPOperation) error {
	if s.entered != nil {
		select {
		case s.entered <- struct{}{}:
		default:
		}
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mcpFailures > 0 {
		s.mcpFailures--
		return errors.New("injected MCP-only failure")
	}
	if s.failures > 0 {
		s.failures--
		return errors.New("injected MCP store failure")
	}
	s.mcpAccepted += len(operations)
	return nil
}

func (s *writerStore) ListMCPOperations(context.Context, routing.WorkspaceSlug, int) ([]executionevidence.CompletedMCPOperation, error) {
	return nil, nil
}

func (s *writerStore) ListMCPOperationPage(context.Context, routing.WorkspaceSlug, MCPCursor, int) (MCPOperationPage, error) {
	return MCPOperationPage{}, nil
}

func (s *writerStore) GetMCPOperation(context.Context, string) (executionevidence.CompletedMCPOperation, bool, error) {
	return executionevidence.CompletedMCPOperation{}, false, nil
}

func TestWriterMCPOverflowRemainsVisibleAfterDrain(t *testing.T) {
	store := &writerStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	writer, err := NewWriter(store, WriterConfig{QueueCapacity: 1, BatchSize: 1, DrainBudget: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	started := time.Now().UTC()
	operation, err := executionevidence.NewCompletedMCPOperation("mcp_test", accessidentity.Local, slug, "docs", "search", started, started.Add(time.Second), execution.Succeeded, execution.Occurred)
	if err != nil {
		t.Fatal(err)
	}
	if !writer.OfferMCP(operation) {
		t.Fatal("first offer dropped")
	}
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not own MCP batch")
	}
	if !writer.OfferMCP(operation) {
		t.Fatal("queue rejected item within capacity")
	}
	if writer.OfferMCP(operation) {
		t.Fatal("full queue accepted offer")
	}
	close(store.release)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	health := writer.Health()
	if health.OfferedMCPOperationsTotal != 3 || health.CommittedMCPOperationsTotal != 2 || health.DroppedMCPOperationsTotal != 1 || health.CommittedExchangesTotal != 0 || health.State != StateDegraded || health.LastError != ErrorHistoryIncomplete {
		t.Fatalf("history gap erased or miscounted: %#v", health)
	}
	if writer.OfferMCP(operation) {
		t.Fatal("MCP offer accepted after close")
	}
}

func TestWriterMCPShutdownLossIsBoundedAndCounted(t *testing.T) {
	store := &writerStore{failures: 1_000_000}
	writer, err := NewWriter(store, WriterConfig{QueueCapacity: 2, BatchSize: 1, DrainBudget: 20 * time.Millisecond, Sleep: func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }})
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	started := time.Now().UTC()
	operation, err := executionevidence.NewCompletedMCPOperation("mcp_loss", accessidentity.Local, slug, "docs", "search", started, started.Add(time.Second), execution.Failed, execution.MayHaveOccurred)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if !writer.OfferMCP(operation) {
			t.Fatal("offer dropped before drain")
		}
	}
	before := time.Now()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(before) > time.Second {
		t.Fatal("MCP shutdown exceeded bound")
	}
	health := writer.Health()
	if health.DroppedMCPOperationsTotal != 2 || health.CommittedMCPOperationsTotal != 0 || health.LastError != ErrorShutdownTimeout {
		t.Fatalf("loss health=%#v", health)
	}
}

func TestWriterMCPRetriesAndMixedBatchCountsRemainDistinct(t *testing.T) {
	store := &writerStore{mcpFailures: 2}
	writer, err := NewWriter(store, WriterConfig{QueueCapacity: 4, BatchSize: 2, BatchWait: time.Millisecond, Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	started := time.Now().UTC()
	operation, err := executionevidence.NewCompletedMCPOperation("mcp_retry", accessidentity.Local, slug, "docs", "search", started, started.Add(time.Second), execution.Succeeded, execution.Occurred)
	if err != nil {
		t.Fatal(err)
	}
	if !writer.Offer(executionevidence.CompletedExchange{}) || !writer.OfferMCP(operation) {
		t.Fatal("mixed offer dropped")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	health := writer.Health()
	if health.CommittedExchangesTotal != 1 || health.CommittedMCPOperationsTotal != 1 || health.DroppedExchangesTotal != 0 || health.DroppedMCPOperationsTotal != 0 || health.State != StateHealthy {
		t.Fatalf("mixed health=%#v", health)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.accepted != 1 || store.mcpAccepted != 1 {
		t.Fatalf("mixed retry replayed committed half: exchange=%d mcp=%d", store.accepted, store.mcpAccepted)
	}
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
	store := &writerStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	w, err := NewWriter(store, WriterConfig{QueueCapacity: 1, BatchSize: 1, DrainBudget: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { close(store.release); _ = w.Close() }()
	first := executionevidence.CompletedExchange{}
	if !w.Offer(first) {
		t.Fatal("first offer dropped")
	}
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not own first batch")
	}
	if !w.Offer(first) {
		t.Fatal("queue rejected item within capacity")
	}
	if w.Offer(first) {
		t.Fatal("full queue accepted third offer")
	}
	health := w.Health()
	if health.QueueCapacity != 1 || health.DroppedExchangesTotal != 1 || health.State != StateDegraded {
		t.Fatalf("health=%#v", health)
	}
}

func TestDroppedLedgerEventDoesNotAffectDurableAccounting(t *testing.T) {
	accountingPath := filepath.Join(t.TempDir(), "accounting.json")
	accountingStore, err := accounting.Open(accountingPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := accountingStore.SetLimit("research", accounting.Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}

	ledgerStore := &writerStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	writer, err := NewWriter(ledgerStore, WriterConfig{QueueCapacity: 1, BatchSize: 1, DrainBudget: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { close(ledgerStore.release); _ = writer.Close() }()
	if !writer.Offer(executionevidence.CompletedExchange{}) {
		t.Fatal("first Ledger offer dropped")
	}
	select {
	case <-ledgerStore.entered:
	case <-time.After(time.Second):
		t.Fatal("Ledger worker did not own first batch")
	}
	if !writer.Offer(executionevidence.CompletedExchange{}) {
		t.Fatal("queue rejected item within capacity")
	}
	if writer.Offer(executionevidence.CompletedExchange{}) {
		t.Fatal("Ledger queue did not exhibit the expected drop")
	}
	if health := writer.Health(); health.DroppedExchangesTotal != 1 || health.State != StateDegraded {
		t.Fatalf("Ledger health = %#v", health)
	}

	if _, err := accountingStore.Reserve("research", "durable-operation", 80, true); err != nil {
		t.Fatal(err)
	}
	if err := accountingStore.Settle("research", "durable-operation", 80, false); err != nil {
		t.Fatal(err)
	}
	reopened, err := accounting.Open(accountingPath)
	if err != nil {
		t.Fatal(err)
	}
	reservations := reopened.Reservations("research")
	if len(reservations) != 1 || reservations[0].AmountCents != 80 || reservations[0].State != "settled" {
		t.Fatalf("accounting after Ledger drop = %#v", reservations)
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
