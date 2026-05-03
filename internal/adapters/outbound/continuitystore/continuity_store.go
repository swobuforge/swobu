// contract's token issuance, mutation, and replay behavior together.
package continuitystore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"reflect"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

const defaultContinuityRetention = 4 * time.Hour

// LocalResponseContinuityStoreConfig configures the in-memory recent replay
// window used for canonical continuation state.
type LocalResponseContinuityStoreConfig struct {
	Retention time.Duration
	Now       func() time.Time
}

// LocalResponseContinuityStore keeps recent canonical continuation chains in
// memory only. v0 continuity is a bounded convenience window, so restart
// forgetfulness is acceptable and much cheaper than carrying a real storage
// engine or a custom on-disk format.
type LocalResponseContinuityStore struct {
	retention time.Duration
	now       func() time.Time

	mu    sync.Mutex
	nodes []chainNode
}

type chainNode struct {
	ID         string
	Namespace  string
	ParentID   string
	ResponseID string
	Model      string
	Items      []compatibility.CanonicalItem
	CreatedAt  time.Time
	LastUsedAt time.Time
}

type indexedContinuityState struct {
	nodesByID        map[string]chainNode
	responseToNodeID map[string]string
	namespaceNodeIDs map[compatibility.ContinuationNamespace][]string
}

// NewLocalResponseContinuityStore builds the in-memory recent replay window.
func NewLocalResponseContinuityStore(cfg LocalResponseContinuityStoreConfig) *LocalResponseContinuityStore {
	retention := cfg.Retention
	if retention <= 0 {
		retention = defaultContinuityRetention
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &LocalResponseContinuityStore{
		retention: retention,
		now:       nowFn,
	}
}

func (s *LocalResponseContinuityStore) Load(_ context.Context, previousResponseID string) (compatibility.ContinuitySnapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.pruneLocked()
	nodeID := index.responseToNodeID[previousResponseID]
	if nodeID == "" {
		return compatibility.ContinuitySnapshot{}, false, nil
	}
	thread, ok := s.materializeThreadLocked(index, nodeID)
	if !ok {
		s.deleteNodeLocked(nodeID)
		return compatibility.ContinuitySnapshot{}, false, nil
	}
	s.touchChainLocked(index, nodeID, s.now().UTC())
	node := index.nodesByID[nodeID]
	return compatibility.NewContinuitySnapshot(node.ResponseID, node.Model, thread), true, nil
}

func (s *LocalResponseContinuityStore) MatchPrefix(_ context.Context, namespace compatibility.ContinuationNamespace, thread []compatibility.CanonicalItem) (compatibility.ContinuationPrefixMatch, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.pruneLocked()
	nodeIDs := index.namespaceNodeIDs[namespace]
	var (
		bestNodeID string
		bestThread []compatibility.CanonicalItem
		bestLen    int
		bestNode   chainNode
	)
	for _, nodeID := range nodeIDs {
		candidateThread, ok := s.materializeThreadLocked(index, nodeID)
		if !ok {
			s.deleteNodeLocked(nodeID)
			continue
		}
		prefixLen := commonPrefixLength(candidateThread, thread)
		if prefixLen == 0 && bestNodeID == "" {
			bestNodeID = nodeID
			bestThread = candidateThread
			bestNode = index.nodesByID[nodeID]
			continue
		}
		if prefixLen > bestLen || (prefixLen == bestLen && newerNode(index.nodesByID[nodeID], bestNode)) {
			bestNodeID = nodeID
			bestThread = candidateThread
			bestLen = prefixLen
			bestNode = index.nodesByID[nodeID]
		}
	}
	if bestNodeID == "" {
		return compatibility.ContinuationPrefixMatch{}, false, nil
	}
	s.touchChainLocked(index, bestNodeID, s.now().UTC())
	return compatibility.ContinuationPrefixMatch{
		Snapshot:     compatibility.NewContinuitySnapshot(bestNode.ResponseID, bestNode.Model, bestThread),
		PrefixLength: bestLen,
	}, true, nil
}

func (s *LocalResponseContinuityStore) Store(_ context.Context, namespace compatibility.ContinuationNamespace, snapshot compatibility.ContinuitySnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.pruneLocked()
	parentID := ""
	items := snapshot.Thread
	bestPrefixLen := 0
	for _, nodeID := range index.namespaceNodeIDs[namespace] {
		candidateThread, ok := s.materializeThreadLocked(index, nodeID)
		if !ok {
			s.deleteNodeLocked(nodeID)
			continue
		}
		prefixLen := commonPrefixLength(candidateThread, snapshot.Thread)
		if prefixLen == len(candidateThread) && prefixLen < len(snapshot.Thread) {
			if parentID == "" || prefixLen > bestPrefixLen || (prefixLen == bestPrefixLen && newerNode(index.nodesByID[nodeID], index.nodesByID[parentID])) {
				parentID = nodeID
				bestPrefixLen = prefixLen
			}
		}
	}
	if bestPrefixLen > 0 {
		items = cloneItems(snapshot.Thread[bestPrefixLen:])
	}
	now := s.now().UTC()
	node := chainNode{
		ID:         newNodeID(),
		Namespace:  namespace.String(),
		ParentID:   parentID,
		ResponseID: snapshot.ResponseID,
		Model:      snapshot.Model,
		Items:      cloneItems(items),
		CreatedAt:  now,
		LastUsedAt: now,
	}
	s.nodes = append(s.nodes, node)
	if parentID != "" {
		s.touchChainLocked(index, parentID, now)
	}
	s.pruneLocked()
	return nil
}

func (s *LocalResponseContinuityStore) pruneLocked() indexedContinuityState {
	cutoff := s.now().UTC().Add(-s.retention)
	kept := make([]chainNode, 0, len(s.nodes))
	for _, node := range s.nodes {
		if !node.LastUsedAt.IsZero() && node.LastUsedAt.Before(cutoff) {
			continue
		}
		kept = append(kept, node)
	}
	s.nodes = kept

	index := s.indexLocked()
	pruned := make([]chainNode, 0, len(s.nodes))
	for _, node := range s.nodes {
		if node.ParentID != "" {
			if _, ok := index.nodesByID[node.ParentID]; !ok {
				continue
			}
		}
		pruned = append(pruned, node)
	}
	s.nodes = pruned
	return s.indexLocked()
}

func (s *LocalResponseContinuityStore) indexLocked() indexedContinuityState {
	index := indexedContinuityState{
		nodesByID:        make(map[string]chainNode, len(s.nodes)),
		responseToNodeID: map[string]string{},
		namespaceNodeIDs: map[compatibility.ContinuationNamespace][]string{},
	}
	for _, node := range s.nodes {
		index.nodesByID[node.ID] = node
		if node.ResponseID != "" {
			existingID := index.responseToNodeID[node.ResponseID]
			if existingID == "" || newerNode(node, index.nodesByID[existingID]) {
				index.responseToNodeID[node.ResponseID] = node.ID
			}
		}
		ns := compatibility.NewContinuationNamespace(node.Namespace)
		index.namespaceNodeIDs[ns] = append(index.namespaceNodeIDs[ns], node.ID)
	}
	return index
}

func (s *LocalResponseContinuityStore) materializeThreadLocked(index indexedContinuityState, nodeID string) ([]compatibility.CanonicalItem, bool) {
	node, ok := index.nodesByID[nodeID]
	if !ok {
		return nil, false
	}
	if node.ParentID == "" {
		return cloneItems(node.Items), true
	}
	parentItems, ok := s.materializeThreadLocked(index, node.ParentID)
	if !ok {
		return nil, false
	}
	return append(parentItems, cloneItems(node.Items)...), true
}

func (s *LocalResponseContinuityStore) touchChainLocked(index indexedContinuityState, nodeID string, ts time.Time) {
	touched := map[string]struct{}{}
	for nodeID != "" {
		if _, ok := touched[nodeID]; ok {
			return
		}
		touched[nodeID] = struct{}{}
		nextID := ""
		for i := range s.nodes {
			if s.nodes[i].ID == nodeID {
				s.nodes[i].LastUsedAt = ts
				nextID = s.nodes[i].ParentID
				break
			}
		}
		if nextID == "" {
			return
		}
		if _, ok := index.nodesByID[nextID]; !ok {
			return
		}
		nodeID = nextID
	}
}

func (s *LocalResponseContinuityStore) deleteNodeLocked(nodeID string) {
	filtered := s.nodes[:0]
	for _, node := range s.nodes {
		if node.ID == nodeID {
			continue
		}
		filtered = append(filtered, node)
	}
	s.nodes = filtered
}

func commonPrefixLength(left []compatibility.CanonicalItem, right []compatibility.CanonicalItem) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		if !reflect.DeepEqual(left[i], right[i]) {
			return i
		}
	}
	return limit
}

func newerNode(left chainNode, right chainNode) bool {
	if right.ID == "" {
		return true
	}
	if left.LastUsedAt.After(right.LastUsedAt) {
		return true
	}
	if left.LastUsedAt.Equal(right.LastUsedAt) && left.CreatedAt.After(right.CreatedAt) {
		return true
	}
	return false
}

func cloneItems(items []compatibility.CanonicalItem) []compatibility.CanonicalItem {
	if items == nil {
		return nil
	}
	out := make([]compatibility.CanonicalItem, len(items))
	for i := range items {
		out[i] = items[i].Clone()
	}
	return out
}

func newNodeID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(raw[:])
}
