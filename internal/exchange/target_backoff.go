package exchange

import (
	"errors"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

const (
	initialTargetBackoff = time.Second
	maximumTargetBackoff = 15 * time.Minute
	targetBackoffStale   = maximumTargetBackoff
)

// targetBackoffKey is the narrowest runtime identity Exchange can prove.
// Editing any effective target setting changes the generation and escapes an
// older observation without coupling the ledger to configuration mutation.
type targetBackoffKey struct {
	workspace     string
	targetID      string
	targetVersion uint64
}

type targetBackoffRecord struct {
	delay time.Duration
	until time.Time
}

// targetObservation identifies one provider call by exact target generation
// and admission order. epoch and suppressed capture the availability knowledge
// present before I/O, so completion time cannot turn stale evidence into a new
// exponential-backoff epoch.
type targetObservation struct {
	key        targetBackoffKey
	sequence   uint64
	epoch      uint64
	suppressed bool
}

type targetObservationState struct {
	nextSequence uint64
	latestResult uint64
	epoch        uint64
	inFlight     uint64
}

type targetGeneration struct {
	targetID      string
	targetVersion uint64
}

// targetBackoffSnapshot is frozen when an exchange starts. Reducer decisions
// never consult concurrently changing process state.
type targetBackoffSnapshot map[targetGeneration]struct{}

func (s targetBackoffSnapshot) active(target routing.Target) bool {
	_, ok := s[targetGeneration{
		targetID: target.ID().String(), targetVersion: uint64(target.Version()),
	}]
	return ok
}

type targetBackoffLedger struct {
	mu           sync.Mutex
	records      map[targetBackoffKey]targetBackoffRecord
	observations map[targetBackoffKey]targetObservationState
	now          func() time.Time
}

func newTargetBackoffLedger() *targetBackoffLedger {
	return &targetBackoffLedger{
		records:      make(map[targetBackoffKey]targetBackoffRecord),
		observations: make(map[targetBackoffKey]targetObservationState),
		now:          time.Now,
	}
}

func (l *targetBackoffLedger) snapshot(workspace string) targetBackoffSnapshot {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.currentTime()
	l.cleanup(now)
	out := make(targetBackoffSnapshot)
	for key, record := range l.records {
		if key.workspace == workspace && now.Before(record.until) {
			out[targetGeneration{targetID: key.targetID, targetVersion: key.targetVersion}] = struct{}{}
		}
	}
	return out
}

func (l *targetBackoffLedger) begin(workspace string, target provider.TargetSnapshot) targetObservation {
	if l == nil {
		return targetObservation{}
	}
	key := targetBackoffKey{workspace: workspace, targetID: target.TargetID, targetVersion: target.TargetVersion}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.currentTime()
	l.cleanup(now)
	state := l.observations[key]
	state.nextSequence++
	state.inFlight++
	l.observations[key] = state
	record := l.records[key]
	return targetObservation{
		key:        key,
		sequence:   state.nextSequence,
		epoch:      state.epoch,
		suppressed: now.Before(record.until),
	}
}

func (l *targetBackoffLedger) observe(observation targetObservation, event exchangeEvent) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	state, ok := l.observations[observation.key]
	if !ok {
		return
	}
	if state.inFlight > 0 {
		state.inFlight--
	}
	if observation.sequence <= state.latestResult {
		l.finishObservation(observation.key, state)
		return
	}
	state.latestResult = observation.sequence
	now := l.currentTime()
	l.cleanup(now)
	switch result := event.(type) {
	case providerIngressReceived:
		delete(l.records, observation.key)
	case providerCallFailed:
		cause := result.failure.Cause()
		var unavailable provider.UnavailableError
		if errors.As(cause, &unavailable) {
			record := l.records[observation.key]
			deadline := record.until
			if observation.epoch == state.epoch && !observation.suppressed {
				record.delay = nextTargetBackoff(record.delay)
				deadline = now.Add(record.delay)
				state.epoch++
			}
			if hinted, ok := provider.RetryNotBefore(cause, now); ok && hinted.After(deadline) {
				deadline = hinted
			}
			record.until = deadline
			if !record.until.IsZero() {
				l.records[observation.key] = record
			}
			l.finishObservation(observation.key, state)
			return
		}
		var rejected provider.RejectedError
		if errors.As(cause, &rejected) {
			delete(l.records, observation.key)
		}
	}
	l.finishObservation(observation.key, state)
}

func (l *targetBackoffLedger) finishObservation(key targetBackoffKey, state targetObservationState) {
	if state.inFlight == 0 {
		if _, active := l.records[key]; !active {
			delete(l.observations, key)
			return
		}
	}
	l.observations[key] = state
}

func (l *targetBackoffLedger) currentTime() time.Time {
	if l.now == nil {
		return time.Now()
	}
	return l.now()
}

func (l *targetBackoffLedger) cleanup(now time.Time) {
	for key, record := range l.records {
		if !now.Before(record.until.Add(targetBackoffStale)) {
			delete(l.records, key)
			if state := l.observations[key]; state.inFlight == 0 {
				delete(l.observations, key)
			}
		}
	}
}

func nextTargetBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return initialTargetBackoff
	}
	if current >= maximumTargetBackoff/2 {
		return maximumTargetBackoff
	}
	return current * 2
}
