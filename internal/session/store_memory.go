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

func NewMemoryStore() Store { return newMemoryStore() }

type memoryStore struct {
	mu        sync.RWMutex
	records   map[workspaceRecordID]Checkpoint
	sessions  map[workspaceSessionID]ClientSession
	byHistory map[workspaceHistoryKey]map[workspaceSessionID]struct{}
	expires   expirationHeap
	now       func() time.Time
}

type workspaceRecordID struct {
	workspaceSlug string
	id            canonical.SwobuResponseID
}

type workspaceSessionID struct {
	workspaceSlug string
	id            ClientSessionID
}

type workspaceHistoryKey struct {
	workspaceSlug string
	history       historyfingerprint.History
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		records: make(map[workspaceRecordID]Checkpoint), sessions: make(map[workspaceSessionID]ClientSession),
		byHistory: make(map[workspaceHistoryKey]map[workspaceSessionID]struct{}),
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
		return "", errors.New("session workspace slug is empty")
	}
	return workspace, nil
}

func (s *memoryStore) Get(_ context.Context, workspace string, id canonical.SwobuResponseID) (Checkpoint, bool, error) {
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

func (s *memoryStore) IsCurrentHead(_ context.Context, workspace string, sessionID ClientSessionID, checkpointID canonical.SwobuResponseID) (bool, error) {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	session, ok := s.sessions[workspaceSessionID{workspaceSlug: workspace, id: sessionID}]
	return ok && session.Head == checkpointID, nil
}

func (s *memoryStore) ResolveHeadByHistory(_ context.Context, workspace string, history historyfingerprint.History) (Checkpoint, HistoryResolution, error) {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return Checkpoint{}, HistoryNotFound, err
	}
	if history.Scheme() == "" {
		return Checkpoint{}, HistoryNotFound, errors.New("session history fingerprint is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	indexKey := workspaceHistoryKey{workspaceSlug: workspace, history: history}
	members := s.byHistory[indexKey]
	available := make([]Checkpoint, 0, len(members))
	for sessionKey := range members {
		session, ok := s.sessions[sessionKey]
		if !ok {
			delete(members, sessionKey)
			continue
		}
		record, ok := s.records[workspaceRecordID{workspaceSlug: workspace, id: session.Head}]
		if !ok {
			delete(members, sessionKey)
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

func (s *memoryStore) StartSession(_ context.Context, workspace string, record Checkpoint) (ClientSession, error) {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return ClientSession{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	if err := s.prepareRecord(&record); err != nil {
		return ClientSession{}, err
	}
	if record.SessionID == "" {
		record.SessionID = ClientSessionID(record.ID)
	}
	sessionKey := workspaceSessionID{workspaceSlug: workspace, id: record.SessionID}
	if _, exists := s.sessions[sessionKey]; exists {
		return ClientSession{}, ErrSessionExists
	}
	if _, exists := s.records[workspaceRecordID{workspaceSlug: workspace, id: record.ID}]; exists {
		return ClientSession{}, ErrCheckpointExists
	}
	session := ClientSession{ID: record.SessionID, Scheme: record.HistoryScheme, Head: record.ID}
	s.storeRecord(workspace, record)
	s.sessions[sessionKey] = session
	s.indexHead(workspace, session, record)
	return session, nil
}

func (s *memoryStore) AdvanceSession(_ context.Context, workspace string, sessionID ClientSessionID, expectedHead canonical.SwobuResponseID, record Checkpoint) error {
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
	sessionKey := workspaceSessionID{workspaceSlug: workspace, id: sessionID}
	current, ok := s.sessions[sessionKey]
	if !ok || current.Head != expectedHead {
		return ErrStaleSessionHead
	}
	if record.SessionID != "" && record.SessionID != sessionID {
		return errors.New("checkpoint session ID does not match advancing session")
	}
	if record.HistoryScheme != current.Scheme {
		return ErrSessionSchemeMismatch
	}
	record.SessionID = sessionID
	if _, exists := s.records[workspaceRecordID{workspaceSlug: workspace, id: record.ID}]; exists {
		return ErrCheckpointExists
	}
	old := s.records[workspaceRecordID{workspaceSlug: workspace, id: current.Head}]
	s.unindexHead(workspace, current, old)
	s.storeRecord(workspace, record)
	current.Head = record.ID
	s.sessions[sessionKey] = current
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
		return fmt.Errorf("invalid session checkpoint response reference: %w", err)
	}
	if record.ID == "" {
		record.ID = response.SwobuID
	}
	if record.ID != response.SwobuID {
		return errors.New("checkpoint ID does not match response")
	}
	if record.ExpiresAt == nil {
		expires := s.now().Add(defaultCheckpointTTL)
		record.ExpiresAt = &expires
	}
	return nil
}

func (s *memoryStore) storeRecord(workspace string, record Checkpoint) {
	if len(s.records) >= maxMemoryStoreRecords {
		s.evictOldest()
	}
	key := workspaceRecordID{workspaceSlug: workspace, id: record.ID}
	cloned := record.Clone()
	s.records[key] = cloned
	heap.Push(&s.expires, expirationEntry{key: key, at: *cloned.ExpiresAt})
}

func (s *memoryStore) indexHead(workspace string, session ClientSession, record Checkpoint) {
	if record.History == nil {
		return
	}
	key := workspaceHistoryKey{workspaceSlug: workspace, history: *record.History}
	if s.byHistory[key] == nil {
		s.byHistory[key] = make(map[workspaceSessionID]struct{})
	}
	s.byHistory[key][workspaceSessionID{workspaceSlug: workspace, id: session.ID}] = struct{}{}
}

func (s *memoryStore) unindexHead(workspace string, session ClientSession, record Checkpoint) {
	if record.History == nil {
		return
	}
	key := workspaceHistoryKey{workspaceSlug: workspace, history: *record.History}
	delete(s.byHistory[key], workspaceSessionID{workspaceSlug: workspace, id: session.ID})
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
	sessionKey := workspaceSessionID{workspaceSlug: key.workspaceSlug, id: record.SessionID}
	if session, ok := s.sessions[sessionKey]; ok && session.Head == record.ID {
		s.unindexHead(key.workspaceSlug, session, record)
		delete(s.sessions, sessionKey)
	}
}
