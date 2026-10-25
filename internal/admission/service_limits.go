// Package admission owns process-local Workspace-wide operation admission.
// Desired policy remains a value in routing.Workspace; no durable rate ledger
// or new authorization relation is introduced here.
package admission

import (
	"errors"
	"math"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
)

var ErrExhausted = errors.New("Workspace Service operational limit exceeded")

// Policy reads the current Workspace lifetime and desired limits at admission.
// It must fail closed when the Workspace no longer exists.
type Policy func(routing.WorkspaceSlug) (string, routing.ServiceLimits, error)

type key struct {
	workspace routing.WorkspaceSlug
	service   routing.ServiceKind
}

type bucket struct {
	incarnation string
	credits     float64
	last        time.Time
	active      uint32
	quota       routing.ServiceQuota
}

// Limiter is one per daemon. The same instance protects local, relay and
// composed Workspace calls, and charges once per downstream operation.
type Limiter struct {
	mu      sync.Mutex
	policy  Policy
	now     func() time.Time
	buckets map[key]*bucket
}

func New(policy Policy) *Limiter {
	return &Limiter{policy: policy, now: time.Now, buckets: make(map[key]*bucket)}
}

// Admit returns a release for the Models concurrency slot. Rate tokens are
// consumed on admission, not refunded when a client disconnects. The release
// is safe to call twice. Models are tracked even while unlimited so enabling a
// concurrent ceiling includes operations admitted under the previous policy.
func (l *Limiter) Admit(workspace routing.WorkspaceSlug, service routing.ServiceKind) (func(), time.Duration, error) {
	noop := func() {}
	if l == nil || l.policy == nil {
		return noop, 0, errors.New("Workspace operational admission unavailable")
	}
	if _, err := routing.ParseServiceKind(string(service)); err != nil {
		return noop, 0, err
	}
	incarnation, limits, err := l.policy(workspace)
	if err != nil {
		return noop, 0, err
	}
	if incarnation == "" {
		return noop, 0, errors.New("Workspace lifetime is missing")
	}
	quota := limits.For(service)
	if quota == (routing.ServiceQuota{}) && service != routing.ServiceModels {
		return noop, 0, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	k := key{workspace, service}
	b := l.buckets[k]
	if b == nil || b.incarnation != incarnation {
		b = &bucket{incarnation: incarnation, credits: float64(quota.RequestsPerMinute), last: now, quota: quota}
		l.buckets[k] = b
	} else {
		// A policy edit does not reset rate credit or in-flight concurrency.
		// Newly raised ceilings may take the newly available budget, but a
		// lower ceiling can never retain excess credit.
		if now.After(b.last) {
			if b.quota.RequestsPerMinute > 0 {
				b.credits = math.Min(float64(b.quota.RequestsPerMinute), b.credits+now.Sub(b.last).Seconds()*float64(b.quota.RequestsPerMinute)/60)
			}
			b.last = now
		}
		if quota.RequestsPerMinute != b.quota.RequestsPerMinute {
			if b.quota.RequestsPerMinute == 0 {
				b.credits = float64(quota.RequestsPerMinute)
			} else {
				b.credits = math.Min(b.credits, float64(quota.RequestsPerMinute))
			}
		}
		b.quota = quota
	}
	if quota.Concurrent > 0 && b.active >= quota.Concurrent {
		return noop, time.Second, ErrExhausted
	}
	if quota.RequestsPerMinute > 0 {
		if b.credits < 1 {
			retry := time.Duration(math.Ceil((1 - b.credits) * float64(time.Minute) / float64(quota.RequestsPerMinute)))
			if retry < time.Second {
				retry = time.Second
			}
			return noop, retry, ErrExhausted
		}
		b.credits--
	}
	if service != routing.ServiceModels {
		return noop, 0, nil
	}
	b.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			if b.active > 0 {
				b.active--
			}
			l.mu.Unlock()
		})
	}, 0, nil
}
