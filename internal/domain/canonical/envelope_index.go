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
	aliases AliasTable
}

// NewEnvelopeIndex creates an empty envelope index.
func NewEnvelopeIndex() *EnvelopeIndex {
	return &EnvelopeIndex{
		entries: map[EnvelopeID]*envelopeIndexEntry{},
		aliases: NewAliasTable(),
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
	default:
		// Other event kinds do not affect closed-envelope tracking.
	}
	if err := i.rememberObservedAliasLocked(ev, entry.kind); err != nil {
		return err
	}
	return nil
}

// RememberAlias stores one native alias for one canonical envelope ID.
func (i *EnvelopeIndex) RememberAlias(key AliasKey, canonicalID EnvelopeID) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.aliases.Remember(key, canonicalID)
}

// ResolveAlias resolves one native alias to its canonical envelope ID.
func (i *EnvelopeIndex) ResolveAlias(key AliasKey) (EnvelopeID, bool) {
	if i == nil {
		return "", false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.aliases.Resolve(key)
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
func (i *EnvelopeIndex) ProjectResponse(id EnvelopeID) (*CanonicalOutputProjection, error) {
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

// rememberObservedAliasLocked records native alias metadata after the canonical
// envelope kind is known so native IDs never replace the canonical envelope ID.
func (i *EnvelopeIndex) rememberObservedAliasLocked(ev Event, kind EnvelopeKind) error {
	if ev.EnvID == "" || kind == "" {
		return nil
	}
	if ev.Meta.Protocol == "" || ev.Meta.NativeID == "" {
		return nil
	}
	key := AliasKey{
		Protocol: ev.Meta.Protocol,
		Kind:     string(kind),
		NativeID: ev.Meta.NativeID,
	}
	if ev.Meta.NativeIndex != nil {
		key.Index = *ev.Meta.NativeIndex
	}
	return i.aliases.Remember(key, ev.EnvID)
}
