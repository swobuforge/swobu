package canonical

import "fmt"

// GrammarValidator enforces the canonical envelope lifecycle rules observed by
// the request-path tests and stream projectors.
type GrammarValidator struct {
	lastSeq int64
	open    map[EnvelopeID]*grammarEnvelopeState
	closed  map[EnvelopeID]EnvelopeKind
}

type grammarEnvelopeState struct {
	kind         EnvelopeKind
	parent       EnvelopeID
	openChildren int
}

// NewGrammarValidator constructs an empty canonical envelope validator.
func NewGrammarValidator() *GrammarValidator {
	return &GrammarValidator{
		open:   map[EnvelopeID]*grammarEnvelopeState{},
		closed: map[EnvelopeID]EnvelopeKind{},
	}
}

// Observe validates one canonical envelope event against ordering and
// lifecycle rules.
func (v *GrammarValidator) Observe(ev Event) error {
	if v == nil {
		return nil
	}
	v.init()
	if ev.Seq <= v.lastSeq {
		return fmt.Errorf("event sequence regressed")
	}
	v.lastSeq = ev.Seq

	switch ev.Kind {
	case EventEnvelopeStart:
		return v.observeStart(ev)
	case EventEnvelopeEnd:
		return v.observeEnd(ev)
	case EventTextDelta:
		return v.requireOpen(ev, EnvMessage, EnvToolResult)
	case EventArgsDelta:
		return v.requireOpen(ev, EnvToolCall)
	case EventUsage, EventFinish, EventError, EventMetadata:
		return v.requireOpen(ev)
	default:
		return fmt.Errorf("event kind %q is unsupported", ev.Kind)
	}
}

func (v *GrammarValidator) init() {
	if v.open == nil {
		v.open = map[EnvelopeID]*grammarEnvelopeState{}
	}
	if v.closed == nil {
		v.closed = map[EnvelopeID]EnvelopeKind{}
	}
}

func (v *GrammarValidator) observeStart(ev Event) error {
	payload, ok := ev.Payload.(EnvelopeStartPayload)
	if !ok {
		return fmt.Errorf("envelope.start payload type %T is unsupported", ev.Payload)
	}
	if payload.Kind == "" {
		return fmt.Errorf("envelope.start kind is required")
	}
	if ev.EnvID == "" {
		return fmt.Errorf("envelope.start env id is required")
	}
	if _, ok := v.open[ev.EnvID]; ok {
		return fmt.Errorf("envelope %q is already open", ev.EnvID)
	}
	if _, ok := v.closed[ev.EnvID]; ok {
		return fmt.Errorf("envelope %q was already closed", ev.EnvID)
	}
	state := &grammarEnvelopeState{kind: payload.Kind, parent: ev.ParentID}
	if ev.ParentID != "" {
		parent, ok := v.open[ev.ParentID]
		if !ok {
			return fmt.Errorf("parent envelope %q is not open", ev.ParentID)
		}
		parent.openChildren++
	}
	v.open[ev.EnvID] = state
	return nil
}

func (v *GrammarValidator) observeEnd(ev Event) error {
	payload, ok := ev.Payload.(EnvelopeEndPayload)
	if !ok {
		return fmt.Errorf("envelope.end payload type %T is unsupported", ev.Payload)
	}
	state, ok := v.open[ev.EnvID]
	if !ok {
		return fmt.Errorf("close for unknown envelope %q", ev.EnvID)
	}
	if payload.Kind != "" && payload.Kind != state.kind {
		return fmt.Errorf("close for envelope %q has mismatched kind", ev.EnvID)
	}
	if state.openChildren > 0 {
		return fmt.Errorf("envelope %q cannot close before its children", ev.EnvID)
	}
	delete(v.open, ev.EnvID)
	v.closed[ev.EnvID] = state.kind
	if state.parent != "" {
		if parent, ok := v.open[state.parent]; ok && parent.openChildren > 0 {
			parent.openChildren--
		}
	}
	return nil
}

func (v *GrammarValidator) requireOpen(ev Event, allowedKinds ...EnvelopeKind) error {
	state, ok := v.open[ev.EnvID]
	if !ok {
		return fmt.Errorf("event %q requires an open envelope", ev.Kind)
	}
	if len(allowedKinds) == 0 {
		return nil
	}
	for _, kind := range allowedKinds {
		if state.kind == kind {
			return nil
		}
	}
	return fmt.Errorf("event %q is not valid for envelope kind %q", ev.Kind, state.kind)
}
