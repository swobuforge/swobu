package ledger

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/outputruntime"
)

const (
	DefaultQueueCapacity       = 4096
	DefaultBatchSize           = 64
	DefaultBatchWait           = 100 * time.Millisecond
	DefaultDrainBudget         = 5 * time.Second
	minRetryDelay              = 100 * time.Millisecond
	maxRetryDelay              = 30 * time.Second
	DefaultMaintenanceInterval = time.Minute
)

// Writer is the bounded asynchronous Ledger runtime. Offer never blocks the
// caller and never performs synchronous fallback I/O. One worker owns Store
// writes, so the Store remains deterministic and directly testable.
type Writer struct {
	store    Store
	worker   *outputruntime.Worker[executionevidence.CompletedExchange]
	capacity int
	now      func() time.Time

	sleep                     func(context.Context, time.Duration) error
	rand                      *rand.Rand
	onHealth                  func(Health)
	closeOnce                 sync.Once
	closeErr                  error
	mu                        sync.RWMutex
	state                     State
	lastError                 ErrorCode
	offered                   atomic.Uint64
	committed                 atomic.Uint64
	dropped                   atomic.Uint64
	maintenanceInterval       time.Duration
	lastMaintenance           time.Time
	retentionFailed           bool
	committedSinceMaintenance uint64
}

// WriterConfig controls only bounded runtime policy. Production defaults are
// the RFC values; tests may shorten delays and drain budgets deterministically.
type WriterConfig struct {
	QueueCapacity       int
	BatchSize           int
	BatchWait           time.Duration
	DrainBudget         time.Duration
	Now                 func() time.Time
	Sleep               func(context.Context, time.Duration) error
	Rand                *rand.Rand
	OnHealth            func(Health)
	MaintenanceInterval time.Duration
}

// Health is a content-free runtime projection suitable for operator status.
type Health struct {
	State                   State     `json:"state"`
	LastError               ErrorCode `json:"last_error,omitempty"`
	QueueDepth              int       `json:"queue_depth"`
	QueueCapacity           int       `json:"queue_capacity"`
	OfferedExchangesTotal   uint64    `json:"offered_exchanges_total"`
	CommittedExchangesTotal uint64    `json:"committed_exchanges_total"`
	DroppedExchangesTotal   uint64    `json:"dropped_exchanges_total"`
}

// NewWriter starts one worker and returns a bounded Ledger runtime.
func NewWriter(store Store, cfg WriterConfig) (*Writer, error) {
	if store == nil {
		return nil, errors.New("Ledger writer requires a Store")
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = DefaultQueueCapacity
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.BatchWait <= 0 {
		cfg.BatchWait = DefaultBatchWait
	}
	if cfg.DrainBudget <= 0 {
		cfg.DrainBudget = DefaultDrainBudget
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepContext
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.New(rand.NewSource(1))
	}
	if cfg.MaintenanceInterval <= 0 {
		cfg.MaintenanceInterval = DefaultMaintenanceInterval
	}
	w := &Writer{store: store, capacity: cfg.QueueCapacity, now: cfg.Now, sleep: cfg.Sleep, rand: cfg.Rand, onHealth: cfg.OnHealth, state: StateHealthy, maintenanceInterval: cfg.MaintenanceInterval, lastMaintenance: cfg.Now()}
	worker, err := outputruntime.NewWorker(outputruntime.WorkerConfig[executionevidence.CompletedExchange]{
		Capacity: cfg.QueueCapacity, Batch: cfg.BatchSize, BatchWait: cfg.BatchWait, Drain: cfg.DrainBudget,
		Handle: func(ctx context.Context, batch []executionevidence.CompletedExchange) error {
			if !w.writeBatch(ctx, batch) {
				return errors.New("ledger write failed")
			}
			return nil
		},
		OnDrop: func(batch []executionevidence.CompletedExchange, _ outputruntime.DropReason) {
			w.dropped.Add(uint64(len(batch)))
			w.mu.Lock()
			w.state = StateDegraded
			w.lastError = ErrorShutdownTimeout
			w.mu.Unlock()
			w.publishHealth()
		},
	})
	if err != nil {
		return nil, err
	}
	w.worker = worker
	w.publishHealth()
	return w, nil
}

// Offer enqueues one completed lifecycle without waiting. A full queue drops
// the newest lifecycle and marks history incomplete through health.
func (w *Writer) Offer(exchange executionevidence.CompletedExchange) bool {
	w.offered.Add(1)
	if !w.worker.Offer(exchange) {
		w.dropped.Add(1)
		w.mu.Lock()
		w.state = StateDegraded
		w.lastError = ErrorHistoryIncomplete
		w.mu.Unlock()
		w.publishHealth()
		return false
	}
	w.publishHealth()
	return true
}

// Close stops accepting work, drains for the configured budget, attempts to
// then closes the Store. It is idempotent.
func (w *Writer) Close() error {
	w.closeOnce.Do(func() { w.worker.Close(); w.closeErr = w.store.Close() })
	return w.closeErr
}

// Health returns a snapshot of content-free runtime counters.
func (w *Writer) Health() Health { return w.health() }

func (w *Writer) writeBatch(ctx context.Context, batch []executionevidence.CompletedExchange) bool {
	if len(batch) == 0 {
		return true
	}
	for attempt := 0; ; attempt++ {
		if err := w.store.WriteBatch(ctx, batch); err == nil {
			w.committed.Add(uint64(len(batch)))
			w.mu.Lock()
			w.committedSinceMaintenance += uint64(len(batch))
			if !w.retentionFailed {
				w.state = StateHealthy
				w.lastError = ""
			}
			w.mu.Unlock()
			w.publishHealth()
			w.mu.Lock()
			due := w.now().Sub(w.lastMaintenance) >= w.maintenanceInterval || w.committedSinceMaintenance >= 1024
			w.mu.Unlock()
			if due {
				if err := w.store.Maintain(ctx, w.now()); err != nil {
					w.mu.Lock()
					w.retentionFailed = true
					w.state = StateDegraded
					w.lastError = ErrorRetentionFailed
					w.mu.Unlock()
					w.publishHealth()
				} else {
					w.mu.Lock()
					w.lastMaintenance = w.now()
					w.committedSinceMaintenance = 0
					w.retentionFailed = false
					w.state = StateHealthy
					w.lastError = ""
					w.mu.Unlock()
					w.publishHealth()
				}
			}
			return true
		} else {
			w.mu.Lock()
			w.state = StateDegraded
			w.lastError = ErrorWriterFailed
			w.mu.Unlock()
			w.publishHealth()
		}
		delay := retryDelay(attempt, w.rand)
		if err := w.sleep(ctx, delay); err != nil {
			w.mu.Lock()
			w.state = StateDegraded
			w.lastError = ErrorShutdownTimeout
			w.mu.Unlock()
			w.publishHealth()
			return false
		}
	}
}

func (w *Writer) health() Health {
	w.mu.RLock()
	defer w.mu.RUnlock()
	depth := 0
	if w.worker != nil {
		depth = w.worker.Depth()
	}
	return Health{State: w.state, LastError: w.lastError, QueueDepth: depth, QueueCapacity: w.capacity, OfferedExchangesTotal: w.offered.Load(), CommittedExchangesTotal: w.committed.Load(), DroppedExchangesTotal: w.dropped.Load()}
}
func (w *Writer) publishHealth() {
	if w.onHealth != nil {
		w.onHealth(w.health())
	}
}
func retryDelay(attempt int, r *rand.Rand) time.Duration {
	delay := minRetryDelay
	for i := 0; i < attempt && delay < maxRetryDelay; i++ {
		delay *= 2
	}
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay/2 + time.Duration(r.Int63n(int64(delay/2)+1))
}
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
