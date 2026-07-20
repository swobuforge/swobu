package canonical

import (
	"context"
	"fmt"
)

// ClosedEnvelope is a fully observed envelope and all descendant events needed
// to project canonical snapshots.
type ClosedEnvelope struct {
	ID     EnvelopeID
	Kind   EnvelopeKind
	Events []Event
}

type envelopeOpenProjection struct {
	kind   EnvelopeKind
	parent EnvelopeID
	evs    []Event
}

// ReadClosedEnvelope consumes events until the requested envelope kind closes.
// It returns io.EOF when no such closed envelope exists in the stream.
func ReadClosedEnvelope(ctx context.Context, reader ResponseStream, kind EnvelopeKind) (*ClosedEnvelope, error) {
	open := map[EnvelopeID]*envelopeOpenProjection{}
	var openResponse EnvelopeID
	appendToAncestors := func(id EnvelopeID, event Event) {
		for current := id; current != ""; {
			state, ok := open[current]
			if !ok {
				break
			}
			state.evs = append(state.evs, event)
			current = state.parent
		}
	}
	for {
		event, err := reader.Next(ctx)
		if err != nil {
			return nil, err
		}
		switch event.Kind {
		case EventEnvelopeStart:
			payload, ok := event.Payload.(EnvelopeStartPayload)
			if !ok {
				return nil, fmt.Errorf("envelope.start payload type %T is unsupported", event.Payload)
			}
			if _, exists := open[event.EnvID]; exists {
				return nil, fmt.Errorf("envelope %q is already open", event.EnvID)
			}
			if payload.Kind == EnvResponse {
				if openResponse != "" {
					return nil, fmt.Errorf("response envelope %q opened while response %q is still open", event.EnvID, openResponse)
				}
				openResponse = event.EnvID
			}
			open[event.EnvID] = &envelopeOpenProjection{kind: payload.Kind, parent: event.ParentID}
			appendToAncestors(event.EnvID, event)
		case EventEnvelopeEnd:
			payload, ok := event.Payload.(EnvelopeEndPayload)
			if !ok {
				return nil, fmt.Errorf("envelope.end payload type %T is unsupported", event.Payload)
			}
			state, ok := open[event.EnvID]
			if !ok {
				return nil, fmt.Errorf("close for unknown envelope %q", event.EnvID)
			}
			appendToAncestors(event.EnvID, event)
			delete(open, event.EnvID)
			if payload.Kind == EnvResponse {
				if openResponse != event.EnvID {
					return nil, fmt.Errorf("response envelope %q does not own the open response stream", event.EnvID)
				}
				openResponse = ""
			}
			if payload.Kind == kind {
				return &ClosedEnvelope{ID: event.EnvID, Kind: kind, Events: state.evs}, nil
			}
		default:
			if _, itemScoped := event.Payload.(ItemEvent); itemScoped {
				if openResponse == "" {
					return nil, fmt.Errorf("%s item event has no open response envelope", event.Kind)
				}
				open[openResponse].evs = append(open[openResponse].evs, event)
			} else {
				appendToAncestors(event.EnvID, event)
			}
		}
	}
}

// ProjectResponse materializes a closed response from completed typed item
// checkpoints. Start and delta events remain delivery evidence and never
// reconstruct durable items.
func (e *ClosedEnvelope) ProjectResponse() (*CanonicalResponse, error) {
	if e == nil || e.Kind != EnvResponse {
		return nil, fmt.Errorf("closed envelope is not a response")
	}
	assembler := newItemStreamAssembler()
	usage := NewUnknownTokenUsage()
	finish := ""
	response := ResponseRef{}
	model := ""
	for _, event := range e.Events {
		switch event.Kind {
		case EventEnvelopeStart:
			payload, ok := event.Payload.(EnvelopeStartPayload)
			if !ok {
				return nil, fmt.Errorf("envelope.start payload type %T is unsupported", event.Payload)
			}
			if payload.Kind == EnvResponse {
				model = payload.Model
			}
		case EventResponseIdentity:
			payload, ok := event.Payload.(ResponseIdentityPayload)
			if !ok {
				return nil, fmt.Errorf("response.identity payload type %T is unsupported", event.Payload)
			}
			response = payload.Response.Clone()
		case EventItemStart, EventContentStart, EventTextDelta, EventArgsDelta, EventItemCompleted:
			itemEvent, ok := event.Payload.(ItemEvent)
			if !ok {
				return nil, fmt.Errorf("%s payload type %T is unsupported", event.Kind, event.Payload)
			}
			if err := assembler.apply(event.Kind, itemEvent); err != nil {
				return nil, err
			}
		case EventUsage:
			payload, ok := event.Payload.(UsagePayload)
			if !ok {
				return nil, fmt.Errorf("usage payload type %T is unsupported", event.Payload)
			}
			usage = payload.Usage
		case EventFinish:
			payload, ok := event.Payload.(FinishPayload)
			if !ok {
				return nil, fmt.Errorf("finish payload type %T is unsupported", event.Payload)
			}
			finish = payload.Reason
		case EventError:
			payload, ok := event.Payload.(ErrorPayload)
			if !ok {
				return nil, fmt.Errorf("error payload type %T is unsupported", event.Payload)
			}
			return nil, fmt.Errorf("canonical response stream error %s: %s", payload.Code, payload.Message)
		case EventEnvelopeEnd:
			// Progressive delivery evidence is intentionally not projection state.
		default:
			return nil, fmt.Errorf("response projection event kind %q is unsupported", event.Kind)
		}
	}
	items, err := assembler.completedItems()
	if err != nil {
		return nil, err
	}
	output, err := NewCanonicalResponse(response, model, items, finish, usage)
	if err != nil {
		return nil, err
	}
	return &output, nil
}
