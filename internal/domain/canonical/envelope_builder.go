package canonical

import (
	"fmt"
	"time"
)

type envelopeState struct {
	kind   EnvelopeKind
	parent EnvelopeID
}

// EnvelopeBuilder mints canonical envelope IDs and emits sequence-monotonic
// lifecycle events. Adapters should use this instead of hand-rolled state
// machines so ID/ordering semantics stay uniform.
type EnvelopeBuilder struct {
	exchangeID string
	seq        int64
	open       map[EnvelopeID]envelopeState
	aliases    AliasTable
	counters   map[EnvelopeKind]int
}

func NewEnvelopeBuilder(exchangeID string) *EnvelopeBuilder {
	return &EnvelopeBuilder{
		exchangeID: exchangeID,
		open:       map[EnvelopeID]envelopeState{},
		aliases:    NewAliasTable(),
		counters:   map[EnvelopeKind]int{},
	}
}

func (b *EnvelopeBuilder) nextSeq() int64 {
	b.seq++
	return b.seq
}

// Start opens a new envelope and validates that its parent is currently open.
func (b *EnvelopeBuilder) Start(kind EnvelopeKind, parent EnvelopeID, attrs EnvelopeStartPayload) (Event, error) {
	id := b.allocateID(kind)
	if parent != "" {
		if _, ok := b.open[parent]; !ok {
			return Event{}, fmt.Errorf("parent envelope %q is not open", parent)
		}
	}
	attrs.Kind = kind
	b.open[id] = envelopeState{kind: kind, parent: parent}
	return Event{
		ExchangeID: b.exchangeID,
		Seq:        b.nextSeq(),
		Time:       time.Now().UTC(),
		Kind:       EventEnvelopeStart,
		EnvID:      id,
		ParentID:   parent,
		Payload:    attrs,
	}, nil
}

// End closes an envelope only when all child envelopes are already closed.
func (b *EnvelopeBuilder) End(id EnvelopeID, status EnvelopeStatus) (Event, error) {
	state, ok := b.open[id]
	if !ok {
		return Event{}, fmt.Errorf("envelope %q is not open", id)
	}
	for childID, child := range b.open {
		if child.parent == id {
			return Event{}, fmt.Errorf("cannot close envelope %q with open child %q", id, childID)
		}
	}
	delete(b.open, id)
	return Event{
		ExchangeID: b.exchangeID,
		Seq:        b.nextSeq(),
		Time:       time.Now().UTC(),
		Kind:       EventEnvelopeEnd,
		EnvID:      id,
		Payload: EnvelopeEndPayload{
			Kind:   state.kind,
			Status: status,
		},
	}, nil
}

// TextDelta emits semantic text for an already-open envelope.
func (b *EnvelopeBuilder) TextDelta(id EnvelopeID, text string) (Event, error) {
	if _, ok := b.open[id]; !ok {
		return Event{}, fmt.Errorf("envelope %q is not open", id)
	}
	return Event{
		ExchangeID: b.exchangeID,
		Seq:        b.nextSeq(),
		Time:       time.Now().UTC(),
		Kind:       EventTextDelta,
		EnvID:      id,
		Payload:    TextDeltaPayload{Text: text},
	}, nil
}

// ArgsDelta emits semantic argument chunks for an already-open envelope.
func (b *EnvelopeBuilder) ArgsDelta(id EnvelopeID, args string) (Event, error) {
	if _, ok := b.open[id]; !ok {
		return Event{}, fmt.Errorf("envelope %q is not open", id)
	}
	return Event{
		ExchangeID: b.exchangeID,
		Seq:        b.nextSeq(),
		Time:       time.Now().UTC(),
		Kind:       EventArgsDelta,
		EnvID:      id,
		Payload:    ArgsDeltaPayload{Args: args},
	}, nil
}

// EnsureResponse returns an open response envelope if present; otherwise it
// allocates a stable canonical response ID for this exchange.
func (b *EnvelopeBuilder) EnsureResponse() EnvelopeID {
	for id, state := range b.open {
		if state.kind == EnvResponse {
			return id
		}
	}
	return b.allocateID(EnvResponse)
}

// EnsureMessage returns the canonical message envelope for a native identity
// key, creating one if this native message has not been observed yet.
func (b *EnvelopeBuilder) EnsureMessage(parent EnvelopeID, role ItemAuthor, key AliasKey) EnvelopeID {
	if id, ok := b.aliases.Get(key); ok {
		return id
	}
	id := b.allocateID(EnvMessage)
	b.aliases.Put(key, id)
	b.open[id] = envelopeState{kind: EnvMessage, parent: parent}
	return id
}

// EnsureToolCall returns the canonical tool-call envelope for a native identity
// key, creating one if this native tool call has not been observed yet.
func (b *EnvelopeBuilder) EnsureToolCall(parent EnvelopeID, key AliasKey) EnvelopeID {
	if id, ok := b.aliases.Get(key); ok {
		return id
	}
	id := b.allocateID(EnvToolCall)
	b.aliases.Put(key, id)
	b.open[id] = envelopeState{kind: EnvToolCall, parent: parent}
	return id
}

// CloseOpenChildren reports currently-open direct children of parent.
// Callers decide closure ordering so protocol-specific end semantics remain at
// adapter edges.
func (b *EnvelopeBuilder) CloseOpenChildren(parent EnvelopeID) []EnvelopeID {
	ids := make([]EnvelopeID, 0)
	for id, state := range b.open {
		if state.parent == parent {
			ids = append(ids, id)
		}
	}
	return ids
}

func (b *EnvelopeBuilder) allocateID(kind EnvelopeKind) EnvelopeID {
	b.counters[kind]++
	prefix := string(kind)
	if b.exchangeID != "" {
		return EnvelopeID(fmt.Sprintf("%s:%s:%d", b.exchangeID, prefix, b.counters[kind]-1))
	}
	return EnvelopeID(fmt.Sprintf("%s:%d", prefix, b.counters[kind]-1))
}
