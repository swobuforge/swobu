package canonical

import "fmt"

type envelopeValidationState struct {
	kind   EnvelopeKind
	parent EnvelopeID
	closed bool
}

// GrammarValidator enforces canonical envelope grammar invariants for one or
// more exchanges.
type GrammarValidator struct {
	open        map[EnvelopeID]envelopeValidationState
	lastSeqByEx map[string]int64
}

func NewGrammarValidator() *GrammarValidator {
	return &GrammarValidator{
		open:        map[EnvelopeID]envelopeValidationState{},
		lastSeqByEx: map[string]int64{},
	}
}

// Observe validates sequence monotonicity, envelope lifecycle, and reference
// integrity for each event.
func (v *GrammarValidator) Observe(ev Event) error {
	if last, ok := v.lastSeqByEx[ev.ExchangeID]; ok && ev.Seq <= last {
		return fmt.Errorf("event sequence must be monotonic, got %d after %d", ev.Seq, last)
	}
	v.lastSeqByEx[ev.ExchangeID] = ev.Seq

	switch ev.Kind {
	case EventEnvelopeStart:
		payload, ok := ev.Payload.(EnvelopeStartPayload)
		if !ok {
			return fmt.Errorf("envelope.start payload type %T is unsupported", ev.Payload)
		}
		if _, exists := v.open[ev.EnvID]; exists {
			return fmt.Errorf("envelope %q started twice", ev.EnvID)
		}
		if ev.ParentID != "" {
			parent, ok := v.open[ev.ParentID]
			if !ok || parent.closed {
				return fmt.Errorf("envelope %q parent %q is not open", ev.EnvID, ev.ParentID)
			}
		}
		v.open[ev.EnvID] = envelopeValidationState{kind: payload.Kind, parent: ev.ParentID}
		return nil
	case EventEnvelopeEnd:
		payload, ok := ev.Payload.(EnvelopeEndPayload)
		if !ok {
			return fmt.Errorf("envelope.end payload type %T is unsupported", ev.Payload)
		}
		state, ok := v.open[ev.EnvID]
		if !ok {
			return fmt.Errorf("end references unknown envelope %q", ev.EnvID)
		}
		if state.kind != payload.Kind {
			return fmt.Errorf("envelope %q kind mismatch: have %q end %q", ev.EnvID, state.kind, payload.Kind)
		}
		for id, other := range v.open {
			if id == ev.EnvID {
				continue
			}
			if other.parent == ev.EnvID && !other.closed {
				return fmt.Errorf("parent envelope %q cannot close before child %q", ev.EnvID, id)
			}
		}
		delete(v.open, ev.EnvID)
		return nil
	case EventTextDelta, EventArgsDelta, EventUsage, EventFinish, EventError, EventMetadata:
		if ev.EnvID == "" {
			return fmt.Errorf("event %q is missing env id", ev.Kind)
		}
		if _, ok := v.open[ev.EnvID]; !ok {
			return fmt.Errorf("event %q references unknown or closed envelope %q", ev.Kind, ev.EnvID)
		}
		return nil
	default:
		return fmt.Errorf("event kind %q is unsupported", ev.Kind)
	}
}
