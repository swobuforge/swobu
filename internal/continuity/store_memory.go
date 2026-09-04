package continuity

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
	"github.com/swobuforge/swobu/internal/domain/thread"
)

const maxMemoryStoreRecords = 1024

func NewMemoryStore() Store { return newMemoryStore() }

type memoryStore struct {
	mu        sync.RWMutex
	records   map[workspaceRecordID]Checkpoint
	threads   map[workspaceThreadID]Thread
	byHistory map[workspaceHistoryKey]map[workspaceThreadID]struct{}
	expires   expirationHeap
	now       func() time.Time
}

type workspaceRecordID struct {
	workspaceSlug string
	id            canonical.SwobuResponseID
}

type workspaceThreadID struct {
	workspaceSlug string
	id            thread.ID
}

type workspaceHistoryKey struct {
	workspaceSlug string
	history       historyfingerprint.History
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		records: make(map[workspaceRecordID]Checkpoint), threads: make(map[workspaceThreadID]Thread),
		byHistory: make(map[workspaceHistoryKey]map[workspaceThreadID]struct{}),
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

func normalizeWorkspace(workspace string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", errors.New("thread workspace slug is empty")
	}
	return workspace, nil
}

func (s *memoryStore) GetCheckpoint(_ context.Context, workspace string, id canonical.SwobuResponseID) (Checkpoint, bool, error) {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return Checkpoint{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	record, ok := s.records[workspaceRecordID{workspaceSlug: workspace, id: id}]
	if !ok {
		return Checkpoint{}, false, nil
	}
	return record.Clone(), true, nil
}

func (s *memoryStore) IsCurrentHead(_ context.Context, workspace string, threadID thread.ID, checkpointID canonical.SwobuResponseID) (bool, error) {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	value, ok := s.threads[workspaceThreadID{workspaceSlug: workspace, id: threadID}]
	return ok && value.Head == checkpointID, nil
}

func (s *memoryStore) GetThread(_ context.Context, workspace string, id thread.ID) (Thread, bool, error) {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return Thread{}, false, err
	}
	if id.IsZero() {
		return Thread{}, false, errors.New("thread ID is zero")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	value, ok := s.threads[workspaceThreadID{workspaceSlug: workspace, id: id}]
	return value, ok, nil
}

func (s *memoryStore) ResolveHeadByHistory(_ context.Context, workspace string, history historyfingerprint.History, preferred thread.ID) (Checkpoint, HistoryResolution, error) {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return Checkpoint{}, HistoryNotFound, err
	}
	if history.Scheme() == "" {
		return Checkpoint{}, HistoryNotFound, errors.New("thread history fingerprint is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	indexKey := workspaceHistoryKey{workspaceSlug: workspace, history: history}
	members := s.byHistory[indexKey]
	available := make([]Checkpoint, 0, len(members))
	for threadKey := range members {
		value, ok := s.threads[threadKey]
		if !ok {
			delete(members, threadKey)
			continue
		}
		if !preferred.IsZero() && value.ID != preferred {
			continue
		}
		record, ok := s.records[workspaceRecordID{workspaceSlug: workspace, id: value.Head}]
		if !ok {
			delete(members, threadKey)
			continue
		}
		available = append(available, record.Clone())
	}
	if len(members) == 0 {
		delete(s.byHistory, indexKey)
	}
	switch len(available) {
	case 0:
		return Checkpoint{}, HistoryNotFound, nil
	case 1:
		return available[0], HistoryUniqueHead, nil
	default:
		return Checkpoint{}, HistoryAmbiguous, nil
	}
}

func (s *memoryStore) StartThread(_ context.Context, workspace string, record Checkpoint) (Thread, error) {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return Thread{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	if err := s.prepareRecord(&record); err != nil {
		return Thread{}, err
	}
	if record.ThreadID.IsZero() {
		return Thread{}, errors.New("thread ID is zero")
	}
	threadKey := workspaceThreadID{workspaceSlug: workspace, id: record.ThreadID}
	if _, exists := s.threads[threadKey]; exists {
		return Thread{}, ErrThreadExists
	}
	if _, exists := s.records[workspaceRecordID{workspaceSlug: workspace, id: record.ResponseID}]; exists {
		return Thread{}, ErrCheckpointExists
	}
	value := Thread{ID: record.ThreadID, Scheme: record.HistoryScheme, Head: record.ResponseID}
	s.storeRecord(workspace, record)
	s.threads[threadKey] = value
	s.indexHead(workspace, value, record)
	return value, nil
}

func (s *memoryStore) AdvanceThread(_ context.Context, workspace string, threadID thread.ID, expectedHead canonical.SwobuResponseID, record Checkpoint) error {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	if err := s.prepareRecord(&record); err != nil {
		return err
	}
	threadKey := workspaceThreadID{workspaceSlug: workspace, id: threadID}
	current, ok := s.threads[threadKey]
	if !ok || current.Head != expectedHead {
		return ErrStaleThreadHead
	}
	if !record.ThreadID.IsZero() && record.ThreadID != threadID {
		return errors.New("checkpoint thread ID does not match advancing thread")
	}
	if record.HistoryScheme != current.Scheme {
		return ErrThreadSchemeMismatch
	}
	record.ThreadID = threadID
	if _, exists := s.records[workspaceRecordID{workspaceSlug: workspace, id: record.ResponseID}]; exists {
		return ErrCheckpointExists
	}
	old := s.records[workspaceRecordID{workspaceSlug: workspace, id: current.Head}]
	s.unindexHead(workspace, current, old)
	s.storeRecord(workspace, record)
	current.Head = record.ResponseID
	s.threads[threadKey] = current
	s.indexHead(workspace, current, record)
	return nil
}

func (s *memoryStore) prepareRecord(record *Checkpoint) error {
	if record.HistoryScheme == "" {
		return errors.New("checkpoint history scheme is empty")
	}
	if record.History != nil && record.History.Scheme() != record.HistoryScheme {
		return errors.New("checkpoint history fingerprint scheme does not match checkpoint scheme")
	}
	response := record.Response.Response()
	if err := response.ValidateCommittedResponse(); err != nil {
		return fmt.Errorf("invalid thread checkpoint response reference: %w", err)
	}
	if record.ResponseID == "" {
		record.ResponseID = response.SwobuID
	}
	if record.ResponseID != response.SwobuID {
		return errors.New("checkpoint ResponseID does not match response")
	}
	if record.ExpiresAt == nil {
		expires := s.now().Add(defaultCheckpointTTL)
		record.ExpiresAt = &expires
	}
	if !record.ExpiresAt.After(s.now()) {
		return errors.New("checkpoint expiration must be in the future")
	}
	return nil
}

func (s *memoryStore) storeRecord(workspace string, record Checkpoint) {
	if len(s.records) >= maxMemoryStoreRecords {
		s.evictOldest()
	}
	key := workspaceRecordID{workspaceSlug: workspace, id: record.ResponseID}
	cloned := record.Clone()
	s.records[key] = cloned
	heap.Push(&s.expires, expirationEntry{key: key, at: *cloned.ExpiresAt})
}

func (s *memoryStore) indexHead(workspace string, value Thread, record Checkpoint) {
	if record.History == nil {
		return
	}
	key := workspaceHistoryKey{workspaceSlug: workspace, history: *record.History}
	if s.byHistory[key] == nil {
		s.byHistory[key] = make(map[workspaceThreadID]struct{})
	}
	s.byHistory[key][workspaceThreadID{workspaceSlug: workspace, id: value.ID}] = struct{}{}
}

func (s *memoryStore) unindexHead(workspace string, value Thread, record Checkpoint) {
	if record.History == nil {
		return
	}
	key := workspaceHistoryKey{workspaceSlug: workspace, history: *record.History}
	delete(s.byHistory[key], workspaceThreadID{workspaceSlug: workspace, id: value.ID})
	if len(s.byHistory[key]) == 0 {
		delete(s.byHistory, key)
	}
}

func (s *memoryStore) reclaimExpired(now time.Time) {
	for s.expires.Len() > 0 && !s.expires[0].at.After(now) {
		entry := heap.Pop(&s.expires).(expirationEntry)
		record, ok := s.records[entry.key]
		if ok && record.ExpiresAt != nil && !record.ExpiresAt.After(now) {
			s.deleteRecord(entry.key, record)
		}
	}
}

func (s *memoryStore) evictOldest() {
	for s.expires.Len() > 0 {
		entry := heap.Pop(&s.expires).(expirationEntry)
		record, ok := s.records[entry.key]
		if !ok || record.ExpiresAt == nil || !record.ExpiresAt.Equal(entry.at) {
			continue
		}
		s.deleteRecord(entry.key, record)
		return
	}
}

func (s *memoryStore) deleteRecord(key workspaceRecordID, record Checkpoint) {
	delete(s.records, key)
	threadKey := workspaceThreadID{workspaceSlug: key.workspaceSlug, id: record.ThreadID}
	if value, ok := s.threads[threadKey]; ok && value.Head == record.ResponseID {
		s.unindexHead(key.workspaceSlug, value, record)
		delete(s.threads, threadKey)
	}
}
