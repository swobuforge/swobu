package canonical

import (
	"fmt"
	"sync"
)

type envelopeIndexEntry struct {
	kind   EnvelopeKind
	parent EnvelopeID
	closed bool
}

// EnvelopeIndex collects observed envelope events and reconstructs closed
// response/request snapshots on demand.
type EnvelopeIndex struct {
	mu      sync.RWMutex
	events  []Event
	entries map[EnvelopeID]*envelopeIndexEntry
}

// NewEnvelopeIndex creates an empty envelope index.
func NewEnvelopeIndex() *EnvelopeIndex {
	return &EnvelopeIndex{
		entries: map[EnvelopeID]*envelopeIndexEntry{},
	}
}

// Observe stores one envelope event for later projection.
func (i *EnvelopeIndex) Observe(ev Event) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.events = append(i.events, ev)
	entry := i.entries[ev.EnvID]
	if entry == nil {
		entry = &envelopeIndexEntry{parent: ev.ParentID}
		i.entries[ev.EnvID] = entry
	}
	if ev.ParentID != "" {
		entry.parent = ev.ParentID
	}
	switch ev.Kind {
	case EventEnvelopeStart:
		if payload, ok := ev.Payload.(EnvelopeStartPayload); ok {
			entry.kind = payload.Kind
		}
	case EventEnvelopeEnd:
		if payload, ok := ev.Payload.(EnvelopeEndPayload); ok {
			entry.kind = payload.Kind
		}
		entry.closed = true
	}
	return nil
}

// Closed returns one closed envelope snapshot when the requested envelope has
// reached a terminal end event.
func (i *EnvelopeIndex) Closed(id EnvelopeID) (*ClosedEnvelope, bool) {
	if i == nil {
		return nil, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	entry, ok := i.entries[id]
	if !ok || !entry.closed {
		return nil, false
	}
	events := make([]Event, 0, len(i.events))
	for _, ev := range i.events {
		if i.isDescendantLocked(ev.EnvID, id) {
			events = append(events, ev)
		}
	}
	return &ClosedEnvelope{ID: id, Kind: entry.kind, Events: events}, true
}

// ProjectResponse reconstructs one closed response envelope into canonical
// output data.
func (i *EnvelopeIndex) ProjectResponse(id EnvelopeID) (*CanonicalOutputData, error) {
	closed, ok := i.Closed(id)
	if !ok {
		return nil, fmt.Errorf("closed response envelope %q not found", id)
	}
	return closed.ProjectResponse()
}

func (i *EnvelopeIndex) isDescendantLocked(child EnvelopeID, ancestor EnvelopeID) bool {
	current := child
	for current != "" {
		if current == ancestor {
			return true
		}
		entry, ok := i.entries[current]
		if !ok {
			return false
		}
		current = entry.parent
	}
	return false
}
