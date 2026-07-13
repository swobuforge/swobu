package turnstate

import (
	"context"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// MemoryContinuationStore is an in-memory implementation of ContinuationStore
// intended for testing and single-node local runs.  It is safe for concurrent
// use but does not survive process restart.
type MemoryContinuationStore struct {
	mu      sync.RWMutex
	records map[string]canonical.ContinuationRecord
}

// NewMemoryContinuationStore creates a fresh in-memory continuation store.
func NewMemoryContinuationStore() *MemoryContinuationStore {
	return &MemoryContinuationStore{
		records: make(map[string]canonical.ContinuationRecord),
	}
}

func (s *MemoryContinuationStore) Put(_ context.Context, rec canonical.ContinuationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.CreatedAt = time.Now().UTC()
	s.records[rec.ID.String()] = rec
	return nil
}

func (s *MemoryContinuationStore) Get(_ context.Context, id canonical.ContinuationID) (canonical.ContinuationRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[id.String()]
	return rec, ok, nil
}

func (s *MemoryContinuationStore) Chain(ctx context.Context, id canonical.ContinuationID) ([]canonical.ContinuationRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var chain []canonical.ContinuationRecord
	current := id
	for !current.IsZero() {
		rec, ok := s.records[current.String()]
		if !ok {
			break
		}
		chain = append([]canonical.ContinuationRecord{rec}, chain...)
		if rec.Parent == nil {
			break
		}
		current = *rec.Parent
	}
	return chain, nil
}
