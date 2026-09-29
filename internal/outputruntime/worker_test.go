package outputruntime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerOwnsBoundedOfferAndDrain(t *testing.T) {
	var handled atomic.Int64
	w, err := NewWorker(WorkerConfig[int]{Capacity: 1, Batch: 2, BatchWait: time.Hour, Drain: time.Second, Handle: func(context.Context, []int) error { handled.Add(1); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Offer(1) {
		t.Fatal("first offer unexpectedly dropped")
	}
	if w.Offer(2) {
		t.Fatal("full queue accepted second offer")
	}
	w.Close()
	if handled.Load() == 0 {
		t.Fatal("worker did not handle queued values")
	}
}

func TestWorkerCloseCancelsRunningBatchAndDrainsWithinBudget(t *testing.T) {
	started := make(chan struct{})
	var canceled atomic.Int64
	var once sync.Once
	w, err := NewWorker(WorkerConfig[int]{Capacity: 2, Batch: 1, BatchWait: time.Millisecond, Drain: 20 * time.Millisecond, Handle: func(ctx context.Context, _ []int) error {
		once.Do(func() { close(started) })
		<-ctx.Done()
		canceled.Add(1)
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Offer(1) {
		t.Fatal("offer rejected")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	done := make(chan struct{})
	go func() { w.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close hung")
	}
	if canceled.Load() < 1 {
		t.Fatal("handler was not canceled")
	}
}

func TestWorkerOfferCloseLinearization(t *testing.T) {
	w, err := NewWorker(WorkerConfig[int]{Capacity: 1, Batch: 1, BatchWait: time.Hour, Drain: time.Second, Handle: func(context.Context, []int) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Offer(1) {
		t.Fatal("offer rejected")
	}
	w.Close()
	if w.Offer(2) {
		t.Fatal("offer accepted after close")
	}
}

func TestWorkerTimerFlushKeepsWorkerAlive(t *testing.T) {
	var handled atomic.Int64
	w, err := NewWorker(WorkerConfig[int]{Capacity: 4, Batch: 4, BatchWait: 5 * time.Millisecond, Drain: time.Second, Handle: func(_ context.Context, batch []int) error { handled.Add(int64(len(batch))); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Offer(1) {
		t.Fatal("first offer rejected")
	}
	time.Sleep(20 * time.Millisecond)
	if !w.Offer(2) {
		t.Fatal("worker died after timer flush")
	}
	time.Sleep(20 * time.Millisecond)
	w.Close()
	if got := handled.Load(); got != 2 {
		t.Fatalf("handled=%d, want 2", got)
	}
}

func TestWorkerContinuesAfterHandleFailure(t *testing.T) {
	var calls, dropped atomic.Int64
	w, err := NewWorker(WorkerConfig[int]{Capacity: 4, Batch: 1, BatchWait: time.Millisecond, Drain: time.Second, Handle: func(_ context.Context, batch []int) error {
		calls.Add(1)
		if batch[0] == 1 {
			return context.DeadlineExceeded
		}
		return nil
	}, OnDrop: func(batch []int, reason DropReason) {
		if reason == DropHandle {
			dropped.Add(int64(len(batch)))
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Offer(1) || !w.Offer(2) {
		t.Fatal("offer rejected")
	}
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	w.Close()
	if calls.Load() < 2 || dropped.Load() != 1 {
		t.Fatalf("calls=%d dropped=%d", calls.Load(), dropped.Load())
	}
}

func TestWorkerShutdownFailureAccountsCurrentAndQueued(t *testing.T) {
	var dropped atomic.Int64
	w, err := NewWorker(WorkerConfig[int]{Capacity: 4, Batch: 4, BatchWait: time.Hour, Drain: 20 * time.Millisecond, Handle: func(ctx context.Context, _ []int) error { <-ctx.Done(); return ctx.Err() }, OnDrop: func(batch []int, _ DropReason) { dropped.Add(int64(len(batch))) }})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if !w.Offer(i) {
			t.Fatal("offer rejected")
		}
	}
	w.Close()
	if got := dropped.Load(); got != 4 {
		t.Fatalf("dropped=%d, want 4", got)
	}
}

func TestWorkerShutdownPreservesBatchInterruptedAfterHandleStarts(t *testing.T) {
	started := make(chan struct{})
	var startOnce sync.Once
	var dropped atomic.Int64
	w, err := NewWorker(WorkerConfig[int]{Capacity: 2, Batch: 1, BatchWait: time.Hour, Drain: 20 * time.Millisecond, Handle: func(ctx context.Context, _ []int) error {
		startOnce.Do(func() { close(started) })
		<-ctx.Done()
		return ctx.Err()
	}, OnDrop: func(batch []int, _ DropReason) { dropped.Add(int64(len(batch))) }})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Offer(1) {
		t.Fatal("offer rejected")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	closed := make(chan struct{})
	go func() { w.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close hung")
	}
	if got := dropped.Load(); got != 1 {
		t.Fatalf("in-flight batch drops=%d, want 1", got)
	}
}
