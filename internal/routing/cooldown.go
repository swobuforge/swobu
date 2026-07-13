package routing

import (
	"context"
	"sync"
	"time"
)

// CooldownStore tracks which targets are temporarily unavailable after
// retryable failures.
type CooldownStore interface {
	Get(ctx context.Context, key TargetKey) (CooldownState, bool)
	Mark(ctx context.Context, key TargetKey, failure FailureClass, ttl time.Duration)
	Clear(ctx context.Context, key TargetKey)
}

// CooldownState records the cooldown condition for one target.
type CooldownState struct {
	FailureClass FailureClass
	ExpiresAt    time.Time
}

// MemoryCooldownStore is an in-memory TTL cooldown store for V0.
// It is safe for concurrent use.
type MemoryCooldownStore struct {
	mu    sync.RWMutex
	slots map[TargetKey]CooldownState
	now   func() time.Time // injectable for tests
}

// NewMemoryCooldownStore creates a new in-memory cooldown store.
func NewMemoryCooldownStore() *MemoryCooldownStore {
	return &MemoryCooldownStore{
		slots: make(map[TargetKey]CooldownState),
		now:   time.Now,
	}
}

// Get returns the cooldown state for a target, if active.
func (s *MemoryCooldownStore) Get(ctx context.Context, key TargetKey) (CooldownState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.slots[key]
	if !ok {
		return CooldownState{}, false
	}
	if s.now().After(state.ExpiresAt) {
		return CooldownState{}, false
	}
	return state, true
}

// Mark records a cooldown entry with the given TTL.
func (s *MemoryCooldownStore) Mark(ctx context.Context, key TargetKey, failure FailureClass, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.slots[key] = CooldownState{
		FailureClass: failure,
		ExpiresAt:    s.now().Add(ttl),
	}
}

// Clear removes any cooldown entry for a target.
func (s *MemoryCooldownStore) Clear(ctx context.Context, key TargetKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.slots, key)
}

// DefaultCooldownDuration is the fallback TTL when no Retry-After is available.
const DefaultCooldownDuration = 30 * time.Second

// MaxCooldownDuration caps cooldown TTL.
const MaxCooldownDuration = 5 * time.Minute

// NormalizeCooldownDuration validates and caps a cooldown duration.
func NormalizeCooldownDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultCooldownDuration
	}
	if d > MaxCooldownDuration {
		return MaxCooldownDuration
	}
	return d
}
