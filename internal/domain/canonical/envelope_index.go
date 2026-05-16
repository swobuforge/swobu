package canonical

import "fmt"

// OpenEnvelope accumulates descendant events while the envelope remains open.
type OpenEnvelope struct {
	ID     EnvelopeID
	Kind   EnvelopeKind
	Parent EnvelopeID
	Events []Event
}

// EnvelopeIndex centralizes stream buffering for projection so adapters do not
// implement parallel buffering state machines.
type EnvelopeIndex struct {
	open   map[EnvelopeID]*OpenEnvelope
	closed map[EnvelopeID]*ClosedEnvelope
}

func NewEnvelopeIndex() *EnvelopeIndex {
	return &EnvelopeIndex{
		open:   map[EnvelopeID]*OpenEnvelope{},
		closed: map[EnvelopeID]*ClosedEnvelope{},
	}
}

// Observe updates open/closed envelope state and stores descendant event
// history needed for later projection of closed envelopes.
func (i *EnvelopeIndex) Observe(ev Event) error {
	switch ev.Kind {
	case EventEnvelopeStart:
		payload, ok := ev.Payload.(EnvelopeStartPayload)
		if !ok {
			return fmt.Errorf("envelope.start payload type %T is unsupported", ev.Payload)
		}
		i.appendToAncestors(ev.ParentID, ev)
		i.open[ev.EnvID] = &OpenEnvelope{ID: ev.EnvID, Kind: payload.Kind, Parent: ev.ParentID, Events: []Event{ev}}
	case EventEnvelopeEnd:
		payload, ok := ev.Payload.(EnvelopeEndPayload)
		if !ok {
			return fmt.Errorf("envelope.end payload type %T is unsupported", ev.Payload)
		}
		i.appendToAncestors(ev.EnvID, ev)
		open, ok := i.open[ev.EnvID]
		if !ok {
			return fmt.Errorf("closing unknown envelope %q", ev.EnvID)
		}
		delete(i.open, ev.EnvID)
		i.closed[ev.EnvID] = &ClosedEnvelope{ID: ev.EnvID, Kind: payload.Kind, Events: append([]Event(nil), open.Events...)}
	default:
		i.appendToAncestors(ev.EnvID, ev)
	}
	return nil
}

func (i *EnvelopeIndex) appendToAncestors(start EnvelopeID, ev Event) {
	cur := start
	for cur != "" {
		open, ok := i.open[cur]
		if !ok {
			return
		}
		open.Events = append(open.Events, ev)
		cur = open.Parent
	}
}

// Closed returns a closed envelope by canonical ID.
func (i *EnvelopeIndex) Closed(id EnvelopeID) (*ClosedEnvelope, bool) {
	out, ok := i.closed[id]
	return out, ok
}

// ProjectResponse materializes a closed canonical response envelope into a
// canonical output snapshot.
func (i *EnvelopeIndex) ProjectResponse(id EnvelopeID) (*CanonicalOutputValue, error) {
	closed, ok := i.closed[id]
	if !ok {
		return nil, fmt.Errorf("response envelope %q is not closed", id)
	}
	return closed.ProjectResponse()
}
