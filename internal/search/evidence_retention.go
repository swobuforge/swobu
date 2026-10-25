package search

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/swobuforge/swobu/internal/platform/atomicfile"
)

// ErrEvidenceRetention reports an unavailable bounded Search history without
// leaking storage details or changing the outcome of an upstream invocation.
var ErrEvidenceRetention = errors.New("Search evidence retention unavailable")

type evidenceSnapshot struct {
	SchemaVersion int        `json:"schema_version"`
	Events        []Evidence `json:"events"`
}

// evidenceRetention owns the existing bounded store's snapshot worker. A single
// pending signal coalesces updates; the authoritative bounded event head remains
// in EvidenceStore, so neither request rate nor disk latency grows a second queue.
type evidenceRetention struct {
	path      string
	updates   chan struct{}
	stop      chan struct{}
	stopped   chan struct{}
	closeOnce sync.Once
	persisted uint64
}

// OpenEvidenceStore reopens a bounded, content-free Search head. Append remains
// memory-only on the caller; one worker atomically replaces snapshots. Close
// flushes the final head after request handlers drain. Abrupt process death can
// lose pending updates, so retained evidence never implies complete capture.
// A corrupt/unreadable existing file is preserved, with explicit retention error
// and live memory capture; it is never silently replaced with an empty history.
func OpenEvidenceStore(path string, limit int) (*EvidenceStore, error) {
	store := NewEvidenceStore(limit)
	raw, err := os.ReadFile(path)
	if err == nil {
		var snapshot evidenceSnapshot
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&snapshot)
		if err == nil {
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				err = ErrEvidenceRetention
			}
		}
		if err == nil && (snapshot.SchemaVersion != 1 || snapshot.Events == nil) {
			err = ErrEvidenceRetention
		}
		if err == nil {
			store.events = snapshot.Events
			if len(store.events) > store.limit {
				store.events = append([]Evidence(nil), store.events[len(store.events)-store.limit:]...)
			}
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		store.retentionError = true
		return store, ErrEvidenceRetention
	}
	worker := &evidenceRetention{path: path, updates: make(chan struct{}, 1), stop: make(chan struct{}), stopped: make(chan struct{})}
	store.retention = worker
	go func() {
		defer close(worker.stopped)
		for {
			select {
			case <-worker.updates:
				store.flushEvidence()
			case <-worker.stop:
				store.flushEvidence()
				return
			}
		}
	}()
	return store, nil
}

func (store *EvidenceStore) flushEvidence() {
	store.mu.RLock()
	generation := store.generation
	if generation == store.retention.persisted {
		store.mu.RUnlock()
		return
	}
	snapshot := evidenceSnapshot{SchemaVersion: 1, Events: append([]Evidence{}, store.events...)}
	store.mu.RUnlock()
	raw, err := json.Marshal(snapshot)
	if err == nil {
		err = atomicfile.Replace(store.retention.path, raw, 0600)
	}
	if err == nil {
		store.retention.persisted = generation
	}
	store.mu.Lock()
	store.retentionError = err != nil
	store.mu.Unlock()
}

// RetentionError reports failed open/write capture independently of live facts.
// False is not a completeness guarantee or a receipt for pending worker writes.
func (store *EvidenceStore) RetentionError() bool {
	if store == nil {
		return true
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.retentionError
}

// Close drains the latest bounded head. Call only after all producers stop;
// repeat calls return the same current retention result without another worker.
func (store *EvidenceStore) Close() error {
	if store == nil {
		return nil
	}
	if worker := store.retention; worker != nil {
		worker.closeOnce.Do(func() { close(worker.stop); <-worker.stopped })
	}
	if store.RetentionError() {
		return ErrEvidenceRetention
	}
	return nil
}
