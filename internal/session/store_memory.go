package session

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// NewMemoryStore returns a thread-safe in-memory Store for production
// bootstrap where persistent session storage is not yet wired. Checkpoints are bounded by
// ExpiresAt and reclaimed opportunistically on every write as well as reads.
func NewMemoryStore() Store {
	return newMemoryStore()
}

type memoryStore struct {
	mu      sync.RWMutex
	records map[workspaceRecordID]Checkpoint
	expires expirationHeap
	now     func() time.Time
}

type workspaceRecordID struct {
	workspaceSlug string
	id            canonical.SwobuResponseID
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		records: make(map[workspaceRecordID]Checkpoint),
		now:     func() time.Time { return time.Now().UTC() },
	}
}

type expirationEntry struct {
	key workspaceRecordID
	at  time.Time
}

type expirationHeap []expirationEntry

func (h expirationHeap) Len() int           { return len(h) }
func (h expirationHeap) Less(i, j int) bool { return h[i].at.Before(h[j].at) }
func (h expirationHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *expirationHeap) Push(value any)    { *h = append(*h, value.(expirationEntry)) }
func (h *expirationHeap) Pop() any {
	old := *h
	last := len(old) - 1
	value := old[last]
	*h = old[:last]
	return value
}

func (s *memoryStore) reclaimExpired(now time.Time) {
	for s.expires.Len() > 0 && !s.expires[0].at.After(now) {
		entry := heap.Pop(&s.expires).(expirationEntry)
		record, found := s.records[entry.key]
		if found && record.ExpiresAt != nil && !record.ExpiresAt.After(now) {
			delete(s.records, entry.key)
		}
	}
}

func (s *memoryStore) Get(ctx context.Context, workspaceSlug string, id canonical.SwobuResponseID) (Checkpoint, bool, error) {
	_ = ctx
	workspaceSlug = strings.TrimSpace(workspaceSlug)
	if workspaceSlug == "" {
		return Checkpoint{}, false, errors.New("session workspace slug is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := workspaceRecordID{workspaceSlug: workspaceSlug, id: id}
	r, ok := s.records[key]
	if !ok {
		return Checkpoint{}, false, nil
	}
	if r.ExpiresAt != nil && !r.ExpiresAt.After(s.now()) {
		delete(s.records, key)
		return Checkpoint{}, false, nil
	}
	return r.Clone(), true, nil
}

func (s *memoryStore) Put(ctx context.Context, workspaceSlug string, record Checkpoint) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.reclaimExpired(now)
	workspaceSlug = strings.TrimSpace(workspaceSlug)
	if workspaceSlug == "" {
		return errors.New("session workspace slug is empty")
	}
	responseRef := record.Response.Response()
	if err := responseRef.ValidateCommittedResponse(); err != nil {
		return fmt.Errorf("invalid session checkpoint response reference: %w", err)
	}
	if err := record.ResolvedMedia.ValidateForRequest(record.Request); err != nil {
		return fmt.Errorf("invalid session checkpoint media: %w", err)
	}
	key := workspaceRecordID{workspaceSlug: workspaceSlug, id: responseRef.SwobuID}
	if _, exists := s.records[key]; exists {
		return ErrCheckpointExists
	}
	cloned := record.Clone()
	if cloned.ExpiresAt == nil {
		expiresAt := now.Add(defaultCheckpointTTL)
		cloned.ExpiresAt = &expiresAt
	}
	s.records[key] = cloned
	heap.Push(&s.expires, expirationEntry{key: key, at: *cloned.ExpiresAt})
	return nil
}
