package replay

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// NewMemoryStore returns a thread-safe in-memory Store for production
// bootstrap where persistent replay is not yet wired. Records are bounded by
// ExpiresAt and purged on read once they go stale.
func NewMemoryStore() Store {
	return newMemoryStore()
}

type memoryStore struct {
	mu      sync.RWMutex
	records map[scopedID]Record
}

type scopedID struct {
	Scope Scope
	ID    ID
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		records: make(map[scopedID]Record),
	}
}

func (s *memoryStore) Get(ctx context.Context, scope Scope, id ID) (Record, bool, error) {
	_ = ctx
	if strings.TrimSpace(scope.Namespace) == "" {
		return Record{}, false, errors.New("replay scope namespace is empty")
	}
	if strings.TrimSpace(scope.CallerKey) == "" {
		return Record{}, false, errors.New("replay scope caller key is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[scopedID{Scope: scope, ID: id}]
	if !ok {
		return Record{}, false, nil
	}
	if r.ExpiresAt != nil && !r.ExpiresAt.After(time.Now().UTC()) {
		delete(s.records, scopedID{Scope: scope, ID: id})
		return Record{}, false, nil
	}
	return r.Clone(), true, nil
}

func (s *memoryStore) Put(ctx context.Context, scope Scope, record Record) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(scope.Namespace) == "" {
		return errors.New("replay scope namespace is empty")
	}
	if strings.TrimSpace(scope.CallerKey) == "" {
		return errors.New("replay scope caller key is empty")
	}
	if record.ID == "" {
		return errors.New("replay record id is empty")
	}
	if record.Scope != scope {
		return errors.New("replay record scope mismatch")
	}
	key := scopedID{Scope: scope, ID: record.ID}
	if _, exists := s.records[key]; exists {
		return ErrReplayRecordExists
	}
	cloned := record.Clone()
	if cloned.ExpiresAt == nil {
		expiresAt := time.Now().UTC().Add(defaultRecordTTL)
		cloned.ExpiresAt = &expiresAt
	}
	s.records[key] = cloned
	return nil
}
