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
)

const maxMemoryStoreRecords = 1024

func NewMemoryStore() Store { return newMemoryStore() }

// historySegment is private physical storage. Checkpoints remain logically
// self-contained while immutable prefixes are shared by ordinary Go pointers.
type historySegment struct {
	parent *historySegment
	items  []canonical.CanonicalItem
	length int
}

type memoryStorageReuse struct {
	store     *memoryStore
	workspace string
	tail      *historySegment
}

func (memoryStorageReuse) isStorageReuseToken() {}

type storedCheckpoint struct {
	checkpoint  Checkpoint
	prelude     []canonical.CanonicalItem
	requestTail *historySegment
	afterTail   *historySegment
}

type memoryStore struct {
	mu             sync.RWMutex
	records        map[workspaceRecordID]storedCheckpoint
	byHistory      map[workspaceHistoryKey]map[canonical.SwobuResponseID]struct{}
	visibleHistory map[workspaceHistoryKey]*historySegment
	expires        expirationHeap
	now            func() time.Time
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
	return &memoryStore{records: make(map[workspaceRecordID]storedCheckpoint), byHistory: make(map[workspaceHistoryKey]map[canonical.SwobuResponseID]struct{}), visibleHistory: make(map[workspaceHistoryKey]*historySegment), now: func() time.Time { return time.Now().UTC() }}
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
		return "", errors.New("checkpoint workspace slug is empty")
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
	return materializeStoredCheckpoint(record), true, nil
}

func (s *memoryStore) FindByHistory(_ context.Context, workspace string, history historyfingerprint.History) (Checkpoint, bool, error) {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return Checkpoint{}, false, err
	}
	if history.Scheme() == "" {
		return Checkpoint{}, false, errors.New("history fingerprint is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	members := s.byHistory[workspaceHistoryKey{workspaceSlug: workspace, history: history}]
	if len(members) != 1 {
		return Checkpoint{}, false, nil
	}
	for id := range members {
		record, ok := s.records[workspaceRecordID{workspaceSlug: workspace, id: id}]
		if !ok {
			return Checkpoint{}, false, nil
		}
		return materializeStoredCheckpoint(record), true, nil
	}
	return Checkpoint{}, false, nil
}

func (s *memoryStore) Put(_ context.Context, workspace string, commit Commit) error {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return err
	}
	record := commit.Checkpoint.Clone()
	if err := s.prepareRecord(&record); err != nil {
		return err
	}
	id := record.Response.Response().SwobuID
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaimExpired(s.now())
	key := workspaceRecordID{workspaceSlug: workspace, id: id}
	if _, exists := s.records[key]; exists {
		return ErrCheckpointExists
	}
	prelude, _, err := canonical.SplitRequestPrelude(record.Request.Items())
	if err != nil {
		return err
	}
	retained := canonical.RetainedHistory(record.Request.Items())
	base, prefix, visibleKey, err := s.resolveStorageReuse(workspace, commit.Reuse, retained)
	if err != nil {
		return err
	}
	requestTail := appendSegment(base, retained[prefix:])
	afterTail := appendSegment(requestTail, record.Response.Items())
	record.Request = record.Request.WithItems(nil)
	record.storageReuse = StorageReuse{token: memoryStorageReuse{store: s, workspace: workspace, tail: afterTail}}
	stored := storedCheckpoint{checkpoint: record, prelude: prelude.Items(), requestTail: requestTail, afterTail: afterTail}
	if len(s.records) >= maxMemoryStoreRecords {
		s.evictOldest()
	}
	s.records[key] = stored
	if visibleKey != nil {
		s.visibleHistory[*visibleKey] = base
	}
	if record.History != nil {
		historyKey := workspaceHistoryKey{workspaceSlug: workspace, history: *record.History}
		if s.byHistory[historyKey] == nil {
			s.byHistory[historyKey] = make(map[canonical.SwobuResponseID]struct{})
		}
		s.byHistory[historyKey][id] = struct{}{}
	}
	heap.Push(&s.expires, expirationEntry{key: key, at: *record.ExpiresAt})
	return nil
}

func (s *memoryStore) prepareRecord(record *Checkpoint) error {
	response := record.Response.Response()
	if err := response.ValidateCommittedResponse(); err != nil {
		return fmt.Errorf("invalid checkpoint response reference: %w", err)
	}
	if record.History != nil && record.History.Scheme() == "" {
		return errors.New("checkpoint history fingerprint is invalid")
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

func appendSegment(parent *historySegment, items []canonical.CanonicalItem) *historySegment {
	if len(items) == 0 {
		return parent
	}
	length := len(items)
	if parent != nil {
		length += parent.length
	}
	return &historySegment{parent: parent, items: append([]canonical.CanonicalItem(nil), items...), length: length}
}

func materializeStoredCheckpoint(record storedCheckpoint) Checkpoint {
	checkpoint := record.checkpoint.Clone()
	items := append(append([]canonical.CanonicalItem(nil), record.prelude...), materializeItems(record.requestTail)...)
	checkpoint.Request = checkpoint.Request.WithItems(items)
	return checkpoint
}

func materializeItems(tail *historySegment) []canonical.CanonicalItem {
	if tail == nil {
		return nil
	}
	items := make([]canonical.CanonicalItem, tail.length)
	for segment := tail; segment != nil; segment = segment.parent {
		copy(items[segment.length-len(segment.items):segment.length], segment.items)
	}
	return items
}

func (s *memoryStore) resolveStorageReuse(workspace string, reuse StorageReuse, retained []canonical.CanonicalItem) (*historySegment, int, *workspaceHistoryKey, error) {
	if reuse.token != nil {
		if reuse.history != nil || reuse.prefixLength != 0 {
			return nil, 0, nil, errors.New("storage reuse evidence is contradictory")
		}
		token, ok := reuse.token.(memoryStorageReuse)
		if !ok || token.store != s {
			return nil, 0, nil, errors.New("storage reuse belongs to another store")
		}
		if token.workspace != workspace {
			return nil, 0, nil, errors.New("storage reuse belongs to another workspace")
		}
		if token.tail == nil {
			return nil, 0, nil, nil
		}
		if token.tail.length > len(retained) {
			return nil, 0, nil, errors.New("storage reuse checkpoint exceeds retained history")
		}
		return token.tail, token.tail.length, nil, nil
	}
	if reuse.history != nil {
		if reuse.prefixLength < 0 || reuse.prefixLength > len(retained) {
			return nil, 0, nil, errors.New("visible-history storage prefix is invalid")
		}
		key := workspaceHistoryKey{workspaceSlug: workspace, history: *reuse.history}
		if existing := s.visibleHistory[key]; existing != nil {
			return existing, reuse.prefixLength, &key, nil
		}
		base := appendSegment(nil, retained[:reuse.prefixLength])
		return base, reuse.prefixLength, &key, nil
	}
	return nil, 0, nil, nil
}

func (s *memoryStore) retainedItemCount() int {
	seen := make(map[*historySegment]struct{})
	count := 0
	for _, record := range s.records {
		for tail := record.afterTail; tail != nil; tail = tail.parent {
			if _, ok := seen[tail]; ok {
				break
			}
			seen[tail] = struct{}{}
			count += len(tail.items)
		}
	}
	for _, root := range s.visibleHistory {
		for tail := root; tail != nil; tail = tail.parent {
			if _, ok := seen[tail]; ok {
				break
			}
			seen[tail] = struct{}{}
			count += len(tail.items)
		}
	}
	return count
}

func (s *memoryStore) reclaimExpired(now time.Time) {
	deleted := false
	for s.expires.Len() > 0 && !s.expires[0].at.After(now) {
		entry := heap.Pop(&s.expires).(expirationEntry)
		record, ok := s.records[entry.key]
		if ok && record.checkpoint.ExpiresAt != nil && !record.checkpoint.ExpiresAt.After(now) {
			s.deleteRecord(entry.key, record)
			deleted = true
		}
	}
	if deleted {
		s.sweepVisibleHistory()
	}
}

func (s *memoryStore) evictOldest() {
	for s.expires.Len() > 0 {
		entry := heap.Pop(&s.expires).(expirationEntry)
		record, ok := s.records[entry.key]
		if !ok || record.checkpoint.ExpiresAt == nil || !record.checkpoint.ExpiresAt.Equal(entry.at) {
			continue
		}
		s.deleteRecord(entry.key, record)
		s.sweepVisibleHistory()
		return
	}
}

func (s *memoryStore) deleteRecord(key workspaceRecordID, record storedCheckpoint) {
	delete(s.records, key)
	if record.checkpoint.History == nil {
		return
	}
	historyKey := workspaceHistoryKey{workspaceSlug: key.workspaceSlug, history: *record.checkpoint.History}
	delete(s.byHistory[historyKey], key.id)
	if len(s.byHistory[historyKey]) == 0 {
		delete(s.byHistory, historyKey)
	}
}

// sweepVisibleHistory makes the optimization index non-owning: only entries
// whose tail is reachable from a live checkpoint remain map-reachable.
func (s *memoryStore) sweepVisibleHistory() {
	reachable := make(map[*historySegment]struct{})
	for _, record := range s.records {
		for tail := record.afterTail; tail != nil; tail = tail.parent {
			if _, ok := reachable[tail]; ok {
				break
			}
			reachable[tail] = struct{}{}
		}
	}
	for key, tail := range s.visibleHistory {
		if _, ok := reachable[tail]; !ok {
			delete(s.visibleHistory, key)
		}
	}
}
