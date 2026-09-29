package outputruntime

import (
	"context"
	"errors"
	"sync"
	"time"
)

type DropReason string

const (
	DropHandle   DropReason = "handle"
	DropShutdown DropReason = "shutdown"
)

type Worker[T any] struct {
	queue         chan T
	batch         int
	wait, drain   time.Duration
	handle        func(context.Context, []T) error
	dropped       func([]T, DropReason)
	mu            sync.RWMutex
	closed        bool
	cancelRunning context.CancelFunc
	stop, stopped chan struct{}
	once          sync.Once
}

type WorkerConfig[T any] struct {
	Capacity, Batch  int
	BatchWait, Drain time.Duration
	Handle           func(context.Context, []T) error
	OnDrop           func([]T, DropReason)
}

func NewWorker[T any](cfg WorkerConfig[T]) (*Worker[T], error) {
	if cfg.Capacity <= 0 || cfg.Batch <= 0 || cfg.Handle == nil {
		return nil, errors.New("invalid output worker configuration")
	}
	if cfg.BatchWait <= 0 {
		cfg.BatchWait = 100 * time.Millisecond
	}
	if cfg.Drain <= 0 {
		cfg.Drain = 5 * time.Second
	}
	w := &Worker[T]{queue: make(chan T, cfg.Capacity), batch: cfg.Batch, wait: cfg.BatchWait, drain: cfg.Drain, handle: cfg.Handle, dropped: cfg.OnDrop, stop: make(chan struct{}), stopped: make(chan struct{})}
	go w.run()
	return w, nil
}

func (w *Worker[T]) Offer(v T) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return false
	}
	select {
	case w.queue <- v:
		return true
	default:
		return false
	}
}
func (w *Worker[T]) Depth() int    { return len(w.queue) }
func (w *Worker[T]) Capacity() int { return cap(w.queue) }
func (w *Worker[T]) Close() {
	w.once.Do(func() {
		w.mu.Lock()
		w.closed = true
		if w.cancelRunning != nil {
			w.cancelRunning()
		}
		close(w.stop)
		w.mu.Unlock()
	})
	<-w.stopped
}

func (w *Worker[T]) run() {
	defer close(w.stopped)
	running, cancel := context.WithCancel(context.Background())
	w.mu.Lock()
	w.cancelRunning = cancel
	closedBeforeStart := w.closed
	w.mu.Unlock()
	if closedBeforeStart {
		cancel()
	}
	defer cancel()
	for {
		select {
		case <-w.stop:
			cancel()
			w.shutdown(nil)
			return
		default:
		}
		select {
		case <-w.stop:
			cancel()
			w.shutdown(nil)
			return
		case first := <-w.queue:
			batch := []T{first}
			timer := time.NewTimer(w.wait)
			for len(batch) < w.batch {
				select {
				case v := <-w.queue:
					batch = append(batch, v)
				case <-timer.C:
					goto flush
				case <-w.stop:
					cancel()
					timer.Stop()
					w.shutdown(batch)
					return
				}
			}
		flush:
			timer.Stop()
			if err := w.handle(running, batch); err != nil {
				if running.Err() != nil {
					w.shutdown(batch)
					return
				}
				if w.dropped != nil {
					w.dropped(batch, DropHandle)
				}
			}
		}
	}
}

func (w *Worker[T]) shutdown(pending []T) {
	ctx, cancel := context.WithTimeout(context.Background(), w.drain)
	defer cancel()
	for {
		batch := pending
		pending = nil
		if batch == nil {
			batch = make([]T, 0, w.batch)
			for len(batch) < w.batch {
				select {
				case v := <-w.queue:
					batch = append(batch, v)
				default:
					goto collected
				}
			}
		}
	collected:
		if len(batch) == 0 {
			return
		}
		if err := w.handle(ctx, batch); err != nil {
			if w.dropped != nil {
				w.dropped(batch, DropShutdown)
			}
			w.dropQueued()
			return
		}
		select {
		case <-ctx.Done():
			remaining := make([]T, 0, len(w.queue))
			for {
				select {
				case v := <-w.queue:
					remaining = append(remaining, v)
				default:
					if len(remaining) > 0 && w.dropped != nil {
						w.dropped(remaining, DropShutdown)
					}
					return
				}
			}
		default:
		}
	}
}

func (w *Worker[T]) dropQueued() {
	if w.dropped == nil {
		return
	}
	remaining := make([]T, 0, len(w.queue))
	for {
		select {
		case v := <-w.queue:
			remaining = append(remaining, v)
		default:
			if len(remaining) > 0 {
				w.dropped(remaining, DropShutdown)
			}
			return
		}
	}
}
