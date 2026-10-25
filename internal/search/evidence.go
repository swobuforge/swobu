package search

import "sync"

// EvidenceStore retains bounded, content-free direct Search lifecycle facts.
// It is intentionally separate from the Exchange evidence store: direct
// Search has no client Exchange identity and must not fabricate one.
type EvidenceStore struct {
	mu             sync.RWMutex
	limit          int
	events         []Evidence
	retention      *evidenceRetention
	retentionError bool
	generation     uint64
}

func NewEvidenceStore(limit int) *EvidenceStore {
	if limit <= 0 {
		limit = 512
	}
	return &EvidenceStore{limit: limit}
}

func (s *EvidenceStore) Append(event Evidence) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.events = append(s.events, event)
	s.generation++
	if len(s.events) > s.limit {
		s.events = append([]Evidence(nil), s.events[len(s.events)-s.limit:]...)
	}
	s.mu.Unlock()
	// Signals carry no history: the worker snapshots the latest bounded head.
	// Slow/unavailable storage cannot block upstream dispatch or trigger fallback.
	if s.retention != nil {
		select {
		case s.retention.updates <- struct{}{}:
		default:
		}
	}
}

func (s *EvidenceStore) Events() []Evidence {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Evidence(nil), s.events...)
}
