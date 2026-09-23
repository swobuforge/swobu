package continuity

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

const defaultMemoryStoreWeight = uint64(256 << 20)

func NewMemoryStore() Store { return newMemoryStore(defaultMemoryStoreWeight) }

// historySegment is private immutable physical storage. Within one chain each
// segment has at most one child: only the chain head may be extended.
type historySegment struct {
	parent *historySegment
	items  []canonical.CanonicalItem
	length int
}

type historyChain struct {
	head          canonical.SwobuResponseID
	anchors       int
	historyWeight uint64
}

type storedCheckpoint struct {
	checkpoint  Checkpoint
	prelude     []canonical.CanonicalItem
	requestTail *historySegment
	afterTail   *historySegment
	chain       *historyChain
	localWeight uint64
	order       *list.Element
}

type memoryStore struct {
	mu         sync.RWMutex
	records    map[workspaceRecordID]*storedCheckpoint
	byHistory  map[workspaceHistoryKey]map[canonical.SwobuResponseID]struct{}
	order      *list.List
	usedWeight uint64
	maxWeight  uint64
}

type workspaceRecordID struct {
	workspaceSlug string
	id            canonical.SwobuResponseID
}

type workspaceHistoryKey struct {
	workspaceSlug string
	history       historyfingerprint.History
}

type orderedRecord struct{ key workspaceRecordID }

func newMemoryStore(maxWeight uint64) *memoryStore {
	return &memoryStore{
		records:   make(map[workspaceRecordID]*storedCheckpoint),
		byHistory: make(map[workspaceHistoryKey]map[canonical.SwobuResponseID]struct{}),
		order:     list.New(),
		maxWeight: maxWeight,
	}
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
	s.mu.RLock()
	record, ok := s.records[workspaceRecordID{workspaceSlug: workspace, id: id}]
	var snapshot storedCheckpoint
	if ok {
		snapshot = *record
	}
	s.mu.RUnlock()
	if !ok {
		return Checkpoint{}, false, nil
	}
	return materializeStoredCheckpoint(snapshot), true, nil
}

func (s *memoryStore) FindByHistory(_ context.Context, workspace string, history historyfingerprint.History) (Checkpoint, bool, error) {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return Checkpoint{}, false, err
	}
	if history.Scheme() == "" {
		return Checkpoint{}, false, errors.New("history fingerprint is invalid")
	}
	s.mu.RLock()
	members := s.byHistory[workspaceHistoryKey{workspaceSlug: workspace, history: history}]
	var snapshot storedCheckpoint
	found := false
	if len(members) == 1 {
		for id := range members {
			if record, ok := s.records[workspaceRecordID{workspaceSlug: workspace, id: id}]; ok {
				snapshot = *record
				found = true
			}
		}
	}
	s.mu.RUnlock()
	if !found {
		return Checkpoint{}, false, nil
	}
	return materializeStoredCheckpoint(snapshot), true, nil
}

func (s *memoryStore) Put(_ context.Context, workspace string, commit Commit) error {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return err
	}
	id := commit.Checkpoint.Response.Response().SwobuID
	key := workspaceRecordID{workspaceSlug: workspace, id: id}
	s.mu.RLock()
	_, duplicate := s.records[key]
	s.mu.RUnlock()
	if duplicate {
		return ErrCheckpointExists
	}
	record := commit.Checkpoint.Clone()
	if err := validateCheckpoint(record); err != nil {
		return err
	}
	prelude, retained, err := canonical.SplitRequestPrelude(record.Request.Items())
	if err != nil {
		return err
	}
	preludeItems := prelude.Items()
	responseItems := record.Response.Items()
	retainedWeights, err := canonicalItemWeights(retained)
	if err != nil {
		return err
	}
	preludeWeight, err := canonicalItemsWeight(preludeItems)
	if err != nil {
		return err
	}
	responseWeight, err := canonicalItemsWeight(responseItems)
	if err != nil {
		return err
	}
	localWeight, err := checkpointLocalWeight(workspace, record, preludeWeight, responseWeight)
	if err != nil {
		return err
	}
	record.Request = record.Request.WithItems(nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.records[key]; exists {
		return ErrCheckpointExists
	}

	var chain *historyChain
	var requestTail, afterTail *historySegment
	var addedHistoryWeight uint64
	predecessor := s.records[workspaceRecordID{workspaceSlug: workspace, id: commit.ResolvedFrom}]
	if !commit.ResolvedFrom.IsZero() && predecessor != nil && predecessor.chain.head == commit.ResolvedFrom {
		prefixLength := predecessor.afterTailLength()
		if prefixLength > len(retained) {
			return fmt.Errorf("checkpoint retained history is shorter than resolved predecessor")
		}
		chain = predecessor.chain
		addedHistoryWeight = historySegmentRetainedWeight(retainedWeights[prefixLength:])
		requestTail = appendSegment(predecessor.afterTail, retained[prefixLength:])
		afterTail = appendSegment(requestTail, responseItems)
		if len(responseItems) > 0 {
			addedHistoryWeight += historySegmentWeight + responseWeight
		}
	} else {
		chain = &historyChain{}
		addedHistoryWeight = historySegmentRetainedWeight(retainedWeights)
		requestTail = appendSegment(nil, retained)
		afterTail = appendSegment(requestTail, responseItems)
		if len(responseItems) > 0 {
			addedHistoryWeight += historySegmentWeight + responseWeight
		}
	}

	stored := &storedCheckpoint{
		checkpoint: record, prelude: preludeItems, requestTail: requestTail,
		afterTail: afterTail, chain: chain, localWeight: localWeight,
	}
	stored.order = s.order.PushBack(orderedRecord{key: key})
	s.records[key] = stored
	if record.History != nil {
		historyKey := workspaceHistoryKey{workspaceSlug: workspace, history: *record.History}
		if s.byHistory[historyKey] == nil {
			s.byHistory[historyKey] = make(map[canonical.SwobuResponseID]struct{})
		}
		s.byHistory[historyKey][id] = struct{}{}
	}
	chain.head = id
	chain.anchors++
	chain.historyWeight += addedHistoryWeight
	s.usedWeight += localWeight + addedHistoryWeight
	s.enforceCapacity()
	return nil
}

func validateCheckpoint(record Checkpoint) error {
	response := record.Response.Response()
	if err := response.ValidateCommittedResponse(); err != nil {
		return fmt.Errorf("invalid checkpoint response reference: %w", err)
	}
	if record.History != nil && record.History.Scheme() == "" {
		return errors.New("checkpoint history fingerprint is invalid")
	}
	return nil
}

func appendSegment(parent *historySegment, items []canonical.CanonicalItem) *historySegment {
	if len(items) == 0 {
		return parent
	}
	detached := append([]canonical.CanonicalItem(nil), items...)
	length := len(detached)
	if parent != nil {
		length += parent.length
	}
	return &historySegment{parent: parent, items: detached, length: length}
}

func (r storedCheckpoint) afterTailLength() int {
	if r.afterTail == nil {
		return 0
	}
	return r.afterTail.length
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

func (s *memoryStore) enforceCapacity() {
	for s.usedWeight > s.maxWeight && len(s.records) > 1 {
		oldest := s.order.Front()
		if oldest == nil {
			return
		}
		key := oldest.Value.(orderedRecord).key
		record := s.records[key]
		if record == nil {
			s.order.Remove(oldest)
			continue
		}
		s.deleteRecord(key, record)
	}
}

func (s *memoryStore) deleteRecord(key workspaceRecordID, record *storedCheckpoint) {
	delete(s.records, key)
	s.order.Remove(record.order)
	s.usedWeight -= record.localWeight
	if record.checkpoint.History != nil {
		historyKey := workspaceHistoryKey{workspaceSlug: key.workspaceSlug, history: *record.checkpoint.History}
		delete(s.byHistory[historyKey], key.id)
		if len(s.byHistory[historyKey]) == 0 {
			delete(s.byHistory, historyKey)
		}
	}
	record.chain.anchors--
	if record.chain.anchors == 0 {
		s.usedWeight -= record.chain.historyWeight
	}
}

func (s *memoryStore) retainedItemCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[*historyChain]struct{})
	count := 0
	for _, record := range s.records {
		if _, ok := seen[record.chain]; ok {
			continue
		}
		if record.checkpoint.Response.Response().SwobuID != record.chain.head {
			continue
		}
		seen[record.chain] = struct{}{}
		for segment := record.afterTail; segment != nil; segment = segment.parent {
			count += len(segment.items)
		}
	}
	return count
}
