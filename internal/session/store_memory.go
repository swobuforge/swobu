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
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

const maxMemoryStoreRecords = 1024

// NewMemoryStore returns a thread-safe in-memory Store for production
// bootstrap where persistent session storage is not yet wired. Checkpoints are bounded by
// ExpiresAt and reclaimed opportunistically on every write as well as reads.
func NewMemoryStore() Store {
	return newMemoryStore()
}

type memoryStore struct {
	mu        sync.RWMutex
	records   map[workspaceRecordID]Checkpoint
	byHistory map[workspaceHistoryKey]map[canonical.SwobuResponseID]struct{}
	expires   expirationHeap
	now       func() time.Time
}

type workspaceRecordID struct {
	workspaceSlug string
	id            canonical.SwobuResponseID
}

type workspaceHistoryKey struct {
	workspaceSlug string
	history       historyfingerprint.History
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		records:   make(map[workspaceRecordID]Checkpoint),
		byHistory: make(map[workspaceHistoryKey]map[canonical.SwobuResponseID]struct{}),
		now:       func() time.Time { return time.Now().UTC() },
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
			s.deleteRecord(entry.key, record)
		}
	}
}

func (s *memoryStore) Get(ctx context.Context, workspaceSlug string, id canonical.SwobuResponseID) (Checkpoint, bool, error) {
	_ = ctx
	workspaceSlug = strings.TrimSpace(workspaceSlug) // swobu:io-string source=boundary
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
		s.deleteRecord(key, r)
		return Checkpoint{}, false, nil
	}
	return r.Clone(), true, nil
}

func (s *memoryStore) FindByHistory(_ context.Context, workspaceSlug string, history historyfingerprint.History) (HistoryMatch, error) {
	workspaceSlug = strings.TrimSpace(workspaceSlug) // swobu:io-string source=boundary
	if workspaceSlug == "" {
		return HistoryMatch{}, errors.New("session workspace slug is empty")
	}
	if history.Scheme() == "" {
		return HistoryMatch{}, errors.New("session history fingerprint is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.reclaimExpired(now)
	indexKey := workspaceHistoryKey{workspaceSlug: workspaceSlug, history: history}
	ids, found := s.byHistory[indexKey]
	if !found {
		return MissingHistoryMatch(), nil
	}
	available := make([]Checkpoint, 0, len(ids))
	for id := range ids {
		recordKey := workspaceRecordID{workspaceSlug: workspaceSlug, id: id}
		record, exists := s.records[recordKey]
		if !exists {
			delete(ids, id)
			continue
		}
		if record.ExpiresAt != nil && !record.ExpiresAt.After(now) {
			s.deleteRecord(recordKey, record)
			continue
		}
		available = append(available, record)
	}
	if len(ids) == 0 {
		delete(s.byHistory, indexKey)
	}
	switch len(available) {
	case 0:
		return MissingHistoryMatch(), nil
	case 1:
		return UniqueHistoryMatch(available[0]), nil
	default:
		return AmbiguousHistoryMatch(), nil
	}
}

func (s *memoryStore) Put(ctx context.Context, workspaceSlug string, record Checkpoint) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.reclaimExpired(now)
	workspaceSlug = strings.TrimSpace(workspaceSlug) // swobu:io-string source=boundary
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
	if len(s.records) >= maxMemoryStoreRecords {
		s.evictOldest()
	}
	s.records[key] = cloned
	if cloned.HistoryFingerprint != nil {
		indexKey := workspaceHistoryKey{workspaceSlug: workspaceSlug, history: *cloned.HistoryFingerprint}
		if s.byHistory[indexKey] == nil {
			s.byHistory[indexKey] = make(map[canonical.SwobuResponseID]struct{})
		}
		s.byHistory[indexKey][responseRef.SwobuID] = struct{}{}
	}
	heap.Push(&s.expires, expirationEntry{key: key, at: *cloned.ExpiresAt})
	return nil
}

func (s *memoryStore) evictOldest() {
	for s.expires.Len() > 0 {
		entry := heap.Pop(&s.expires).(expirationEntry)
		record, found := s.records[entry.key]
		if !found || record.ExpiresAt == nil || !record.ExpiresAt.Equal(entry.at) {
			continue
		}
		s.deleteRecord(entry.key, record)
		return
	}
}

// deleteRecord keeps the primary record and secondary membership set in one
// lock-owned mutation. Other indistinguishable records survive independently.
func (s *memoryStore) deleteRecord(key workspaceRecordID, record Checkpoint) {
	delete(s.records, key)
	if record.HistoryFingerprint == nil {
		return
	}
	indexKey := workspaceHistoryKey{workspaceSlug: key.workspaceSlug, history: *record.HistoryFingerprint}
	if indexedIDs, found := s.byHistory[indexKey]; found {
		delete(indexedIDs, key.id)
		if len(indexedIDs) == 0 {
			delete(s.byHistory, indexKey)
		}
	}
}
