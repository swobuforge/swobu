package ledger

import (
	"context"
	"sync"
)

// MemoryStore is an in-process bounded ledger implementation.
type MemoryStore[T any] struct {
	mu      sync.RWMutex
	records []T
}

func NewMemoryStore[T any]() *MemoryStore[T] {
	return &MemoryStore[T]{records: make([]T, 0, 128)}
}

func (s *MemoryStore[T]) Append(_ context.Context, record T) {
	s.mu.Lock()
	s.records = append(s.records, record)
	s.mu.Unlock()
}

func (s *MemoryStore[T]) List(_ context.Context, limit int) ([]T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit >= len(s.records) {
		out := make([]T, len(s.records))
		copy(out, s.records)
		return out, nil
	}
	start := len(s.records) - limit
	out := make([]T, limit)
	copy(out, s.records[start:])
	return out, nil
}
