package canonical

import "fmt"

// responseFold is the single interpreter for events that change canonical
// response meaning. Retained and streaming projection own different envelope
// storage/lifecycle strategies but feed response semantics through this fold.
type responseFold struct {
	assembler  *itemStreamAssembler
	usage      TokenUsage
	completion Completion
	response   ResponseRef
	model      string
}

func newResponseFold() responseFold {
	return responseFold{
		assembler: newItemStreamAssembler(),
		usage:     NewUnknownTokenUsage(),
	}
}

func (f *responseFold) start(payload EnvelopeStartPayload) error {
	if payload.Kind != EnvResponse {
		return fmt.Errorf("response fold requires response envelope, got kind %q", payload.Kind)
	}
	f.model = payload.Model
	return nil
}

func (f *responseFold) apply(event Event) error {
	switch event.Kind {
	case EventResponseIdentity:
		payload, ok := event.Payload.(ResponseIdentityPayload)
		if !ok {
			return fmt.Errorf("response.identity payload type %T is unsupported", event.Payload)
		}
		f.response = payload.Response.Clone()
	case EventItemStart, EventContentStart, EventTextDelta, EventArgsDelta, EventItemCompleted:
		itemEvent, ok := event.Payload.(ItemEvent)
		if !ok {
			return fmt.Errorf("%s payload type %T is unsupported", event.Kind, event.Payload)
		}
		return f.assembler.apply(event.Kind, itemEvent)
	case EventUsage:
		payload, ok := event.Payload.(UsagePayload)
		if !ok {
			return fmt.Errorf("usage payload type %T is unsupported", event.Payload)
		}
		f.usage = payload.Usage
	case EventFinish:
		payload, ok := event.Payload.(FinishPayload)
		if !ok {
			return fmt.Errorf("finish payload type %T is unsupported", event.Payload)
		}
		f.completion = payload.Completion
	case EventError:
		payload, ok := event.Payload.(ErrorPayload)
		if !ok {
			return fmt.Errorf("error payload type %T is unsupported", event.Payload)
		}
		return fmt.Errorf("canonical response stream error %s: %s", payload.Code, payload.Message)
	default:
		return fmt.Errorf("response projection event kind %q is unsupported", event.Kind)
	}
	return nil
}

func (f *responseFold) responseOutput() (*CanonicalResponse, error) {
	items, err := f.assembler.completedItems()
	if err != nil {
		return nil, err
	}
	output, err := NewCanonicalResponse(f.response, f.model, items, f.completion, f.usage)
	if err != nil {
		return nil, err
	}
	return &output, nil
}
