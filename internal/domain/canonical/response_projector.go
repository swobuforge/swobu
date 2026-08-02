package canonical

import (
	"context"
	"fmt"
)

// ResponseProjector folds a canonical response stream into projection state
// incrementally — one event at a time — so a caller can observe a streaming
// response without retaining the event slice. The projected response is
// materialized once, at the end, from the folded state.
//
// The fold is exactly the projection ReadClosedEnvelope+ProjectResponse would
// produce from the full event slice (envelope_projector.go), minus the slice
// retention: item start/delta events fold into an itemStreamAssembler (state,
// not events), and only the scalar identity/model/usage/completion facts are
// kept. Memory therefore scales with the number of completed items, not with
// the number of streamed deltas.
//
// It validates a single open response envelope: EventResponseIdentity must
// match the configured binding the caller passes to Apply, and the stream must
// end with an EventEnvelopeEnd of kind EnvResponse.
type ResponseProjector struct {
	binding ResponseBinding
	fold    responseFold
	// state of the open response envelope; "" before start, set after
	// EventEnvelopeStart, cleared after the matching EventEnvelopeEnd.
	openResponse EnvelopeID
	started      bool
	ended        bool
	err          error
}

// NewResponseProjector returns a projector that will reject a response whose
// identity does not match binding. A zero binding disables the identity check
// (useful for callers that project a stream they have already validated).
func NewResponseProjector(binding ResponseBinding) *ResponseProjector {
	return &ResponseProjector{
		binding: binding,
		fold:    newResponseFold(),
	}
}

// Apply folds one canonical event into projection state. It is called once per
// event, in order, as a response stream is read; events are not retained. A
// non-nil error means the stream cannot produce a valid response; the caller
// should treat the projection as failed exactly as ReadClosedEnvelope would.
func (p *ResponseProjector) Apply(event Event) error {
	if p.err != nil {
		return p.err
	}
	if p.ended {
		return fmt.Errorf("response projector received event %q after envelope end", event.Kind)
	}
	switch event.Kind {
	case EventEnvelopeStart:
		payload, ok := event.Payload.(EnvelopeStartPayload)
		if !ok {
			return p.fail(fmt.Errorf("envelope.start payload type %T is unsupported", event.Payload))
		}
		if payload.Kind != EnvResponse {
			return p.fail(fmt.Errorf("projector is scoped to response envelopes, got kind %q", payload.Kind))
		}
		if p.started {
			return p.fail(fmt.Errorf("response envelope %q opened while %q is still open", event.EnvID, p.openResponse))
		}
		p.openResponse = event.EnvID
		p.started = true
		if err := p.fold.start(payload); err != nil {
			return p.fail(err)
		}
	case EventResponseIdentity:
		payload, ok := event.Payload.(ResponseIdentityPayload)
		if !ok {
			return p.fail(fmt.Errorf("response.identity payload type %T is unsupported", event.Payload))
		}
		if p.binding.SwobuID != "" && payload.Response.SwobuID != p.binding.SwobuID {
			return p.fail(fmt.Errorf("response identity %q does not match checkpoint %q", payload.Response.SwobuID, p.binding.SwobuID))
		}
		if err := p.fold.apply(event); err != nil {
			return p.fail(err)
		}
	case EventItemStart, EventContentStart, EventTextDelta, EventArgsDelta, EventItemCompleted, EventUsage, EventFinish, EventError:
		if err := p.fold.apply(event); err != nil {
			return p.fail(err)
		}
	case EventEnvelopeEnd:
		payload, ok := event.Payload.(EnvelopeEndPayload)
		if !ok {
			return p.fail(fmt.Errorf("envelope.end payload type %T is unsupported", event.Payload))
		}
		if payload.Kind != EnvResponse {
			return p.fail(fmt.Errorf("projector is scoped to response envelopes, got end kind %q", payload.Kind))
		}
		if !p.started || p.openResponse != event.EnvID {
			return p.fail(fmt.Errorf("response envelope end %q does not match the open response", event.EnvID))
		}
		if payload.Status != EnvelopeStatusCompleted {
			return p.fail(fmt.Errorf("canonical response did not complete successfully: %s", payload.Status))
		}
		p.openResponse = ""
		p.ended = true
	default:
		return p.fail(fmt.Errorf("response projection event kind %q is unsupported", event.Kind))
	}
	return nil
}

// Done materializes the projected response from folded state. It must be called
// after the terminal EventEnvelopeEnd. It errors if the stream was not a
// completed response envelope, mirroring ProjectResponse.
func (p *ResponseProjector) Done() (*CanonicalResponse, error) {
	if p.err != nil {
		return nil, p.err
	}
	if !p.ended {
		return nil, fmt.Errorf("response projector was not closed by a completed envelope end")
	}
	return p.fold.responseOutput()
}

// ProjectStream is a convenience that drains a whole ResponseStream through a
// one-shot projector and returns the completed response. It is the incremental
// equivalent of ReadClosedEnvelope(reader, EnvResponse).ProjectResponse() but
// retains no event slice.
func ProjectStream(ctx context.Context, reader ResponseStream, binding ResponseBinding) (*CanonicalResponse, error) {
	projector := NewResponseProjector(binding)
	for {
		event, err := reader.Next(ctx)
		if err != nil {
			return nil, err
		}
		if err := projector.Apply(event); err != nil {
			return nil, err
		}
		if event.Kind == EventEnvelopeEnd {
			return projector.Done()
		}
	}
}

func (p *ResponseProjector) fail(err error) error {
	p.err = err
	return err
}
