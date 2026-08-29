package exchange

import (
	"sync"

	"github.com/swobuforge/swobu/internal/provider"
)

type targetExceptionGeneration struct {
	workspace     string
	targetID      string
	targetVersion uint64
}

type targetFactKey struct {
	generation targetExceptionGeneration
	fact       provider.TargetFact
}

// targetExceptions is the RequestIngress-owned process-lifetime set of proven
// preferred-wire rejections. Absence means optimistic preferred projection.
// Routing generations invalidate exceptions; no positive support state, TTL,
// persistence, or package global state exists.
type targetExceptions struct {
	mu     sync.RWMutex
	values map[targetFactKey]struct{}
}

func newTargetExceptions() *targetExceptions {
	return &targetExceptions{values: make(map[targetFactKey]struct{})}
}

func (k *targetExceptions) lookup(generation targetExceptionGeneration, fact provider.TargetFact) (bool, bool) {
	if k == nil {
		return false, false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	_, rejected := k.values[targetFactKey{generation: generation, fact: fact}]
	if rejected {
		return false, true
	}
	return false, false
}

func (k *targetExceptions) observe(generation targetExceptionGeneration, fact provider.TargetFact, preferredAccepted bool) {
	if k == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	key := targetFactKey{generation: generation, fact: fact}
	if preferredAccepted {
		delete(k.values, key)
		return
	}
	k.values[key] = struct{}{}
}

func targetFactsForAttempt(exceptions *targetExceptions, generation targetExceptionGeneration) *provider.TargetFacts {
	return provider.NewTargetFacts(func(fact provider.TargetFact) (bool, bool) {
		return exceptions.lookup(generation, fact)
	})
}

func cloneFactReads(reads map[provider.TargetFact]bool) map[provider.TargetFact]bool {
	if len(reads) == 0 {
		return nil
	}
	out := make(map[provider.TargetFact]bool, len(reads))
	for fact, value := range reads {
		out[fact] = value
	}
	return out
}
