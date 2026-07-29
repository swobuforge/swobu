package canonical

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// ValidatedResponseStream enforces response/item/content lifecycle invariants
// before each event reaches either client projection or checkpoint commit.
type ValidatedResponseStream struct {
	upstream     ResponseStream
	responseID   EnvelopeID
	identitySeen bool
	itemsSeen    bool
	finishSeen   bool
	completion   Completion
	usageSeen    bool
	errorSeen    bool
	assembler    *itemStreamAssembler
	responseDone bool
	lastSequence int64
	effectGuard  responseEffectGuard
}

// NewValidatedResponseStream wraps provider-decoded canonical events at the
// shared delivery/checkpoint seam.
func NewValidatedResponseStream(upstream ResponseStream) *ValidatedResponseStream {
	return &ValidatedResponseStream{upstream: upstream}
}

func (r *ValidatedResponseStream) Next(ctx context.Context) (Event, error) {
	event, err := r.upstream.Next(ctx)
	if errors.Is(err, io.EOF) {
		if !r.responseDone {
			return Event{}, fmt.Errorf("provider response stream ended before terminal response envelope")
		}
		return Event{}, io.EOF
	}
	if err != nil {
		return Event{}, err
	}
	if err := r.apply(event); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (r *ValidatedResponseStream) Close(ctx context.Context) error { return r.upstream.Close(ctx) }

func (r *ValidatedResponseStream) apply(event Event) error {
	if r.responseDone {
		return fmt.Errorf("%s arrived after response completion", event.Kind)
	}
	if event.Seq > 0 {
		if event.Seq <= r.lastSequence {
			return fmt.Errorf("response event sequence regressed from %d to %d", r.lastSequence, event.Seq)
		}
		r.lastSequence = event.Seq
	}
	switch event.Kind {
	case EventEnvelopeStart:
		return r.applyEnvelopeStart(event)
	case EventResponseIdentity:
		return r.applyResponseIdentity(event)
	case EventItemStart, EventContentStart, EventTextDelta, EventArgsDelta, EventItemCompleted:
		return r.applyItemEvent(event)
	case EventUsage:
		return r.applyUsage(event)
	case EventFinish:
		return r.applyFinish(event)
	case EventError:
		return r.applyError(event)
	case EventEnvelopeEnd:
		return r.applyEnvelopeEnd(event)
	default:
		return fmt.Errorf("response event kind %q is unsupported", event.Kind)
	}
}

func (r *ValidatedResponseStream) applyEnvelopeStart(event Event) error {
	payload, ok := event.Payload.(EnvelopeStartPayload)
	if !ok {
		return fmt.Errorf("envelope.start payload type %T is unsupported", event.Payload)
	}
	if payload.Kind != EnvResponse {
		return fmt.Errorf("envelope kind %q is unsupported", payload.Kind)
	}
	if event.EnvID == "" || event.ParentID != "" || r.responseID != "" {
		return fmt.Errorf("response envelope %q opened outside the single-response stream contract", event.EnvID)
	}
	r.responseID = event.EnvID
	r.assembler = newItemStreamAssembler()
	r.effectGuard = responseEffectGuard{}
	return nil
}

func (r *ValidatedResponseStream) applyResponseIdentity(event Event) error {
	if r.responseID == "" || r.assembler == nil {
		return fmt.Errorf("response.identity has no open response envelope")
	}
	if r.identitySeen {
		return fmt.Errorf("response.identity is duplicated")
	}
	if r.itemsSeen {
		return fmt.Errorf("response.identity arrived after item delivery")
	}
	payload, ok := event.Payload.(ResponseIdentityPayload)
	if !ok {
		return fmt.Errorf("response.identity payload type %T is unsupported", event.Payload)
	}
	if payload.Response.SwobuID.IsZero() {
		return fmt.Errorf("response.identity requires a non-empty Swobu response ID")
	}
	if event.ParentID != "" || event.EnvID != "" && event.EnvID != r.responseID {
		return fmt.Errorf("response.identity envelope %q contradicts response %q", event.EnvID, r.responseID)
	}
	r.identitySeen = true
	return nil
}

func (r *ValidatedResponseStream) applyItemEvent(event Event) error {
	if err := r.requireOpenResponseEvent(event); err != nil {
		return err
	}
	if r.finishSeen || r.errorSeen {
		return fmt.Errorf("%s arrived after terminal response semantics", event.Kind)
	}
	itemEvent, ok := event.Payload.(ItemEvent)
	if !ok {
		return fmt.Errorf("%s payload type %T is unsupported", event.Kind, event.Payload)
	}
	if event.EnvID != "" || event.ParentID != "" {
		return fmt.Errorf("%s must be addressed only by ItemPosition", event.Kind)
	}
	if err := validateResponseItemEvent(event.Kind, itemEvent); err != nil {
		return err
	}
	if err := r.assembler.apply(event.Kind, itemEvent); err != nil {
		return err
	}
	if event.Kind == EventItemCompleted {
		completed := itemEvent.Payload.(ItemCompletedPayload)
		if err := r.effectGuard.Accept(int(itemEvent.Position.Item), completed.Item); err != nil {
			return fmt.Errorf("response tool lifecycle at item %d is invalid: %w", itemEvent.Position.Item, err)
		}
	}
	r.itemsSeen = true
	return nil
}

func validateResponseItemEvent(kind EventKind, event ItemEvent) error {
	if kind == EventItemStart {
		if start, ok := event.Payload.(ItemStartPayload); ok {
			if message, ok := start.Message(); ok && message.Author != MessageRoleAssistant {
				return fmt.Errorf("response message item %d must be assistant-authored", event.Position.Item)
			}
		}
	}
	if kind == EventItemCompleted {
		if completed, ok := event.Payload.(ItemCompletedPayload); ok {
			return validateResponseItem(int(event.Position.Item), completed.Item)
		}
	}
	return nil
}

func (r *ValidatedResponseStream) applyUsage(event Event) error {
	if err := r.requireOpenResponseEvent(event); err != nil {
		return err
	}
	if r.usageSeen {
		return fmt.Errorf("response usage is duplicated")
	}
	if r.errorSeen {
		return fmt.Errorf("response usage arrived after terminal error")
	}
	if _, ok := event.Payload.(UsagePayload); !ok {
		return fmt.Errorf("usage payload type %T is unsupported", event.Payload)
	}
	r.usageSeen = true
	return nil
}

func (r *ValidatedResponseStream) applyFinish(event Event) error {
	if err := r.requireOpenResponseEvent(event); err != nil {
		return err
	}
	if r.finishSeen {
		return fmt.Errorf("response finish is duplicated")
	}
	if r.errorSeen {
		return fmt.Errorf("response finish conflicts with terminal error")
	}
	payload, ok := event.Payload.(FinishPayload)
	if !ok {
		return fmt.Errorf("finish payload type %T is unsupported", event.Payload)
	}
	if err := payload.Completion.validate(); err != nil {
		return err
	}
	r.finishSeen = true
	r.completion = payload.Completion
	return nil
}

func (r *ValidatedResponseStream) applyError(event Event) error {
	if err := r.requireOpenResponseEvent(event); err != nil {
		return err
	}
	if r.errorSeen {
		return fmt.Errorf("response error is duplicated")
	}
	if r.finishSeen {
		return fmt.Errorf("response error conflicts with finish")
	}
	if _, ok := event.Payload.(ErrorPayload); !ok {
		return fmt.Errorf("error payload type %T is unsupported", event.Payload)
	}
	r.errorSeen = true
	return nil
}

func (r *ValidatedResponseStream) applyEnvelopeEnd(event Event) error {
	payload, ok := event.Payload.(EnvelopeEndPayload)
	if !ok {
		return fmt.Errorf("envelope.end payload type %T is unsupported", event.Payload)
	}
	if payload.Kind != EnvResponse || r.responseID == "" || event.EnvID != r.responseID || event.ParentID != "" {
		return fmt.Errorf("response envelope %q closed without ownership", event.EnvID)
	}
	if err := r.validateTerminalStatus(payload.Status); err != nil {
		return err
	}
	r.responseID = ""
	r.responseDone = true
	return nil
}

func (r *ValidatedResponseStream) validateTerminalStatus(status EnvelopeStatus) error {
	switch status {
	case EnvelopeStatusCompleted:
		if !r.identitySeen {
			return fmt.Errorf("response completed without response.identity")
		}
		if _, err := r.assembler.completedItems(); err != nil {
			return err
		}
		if r.errorSeen {
			return fmt.Errorf("completed response conflicts with terminal error")
		}
		if !r.finishSeen {
			return fmt.Errorf("completed response requires finish")
		}
		if r.completion.Class() == CompletionCompleted {
			if err := r.effectGuard.RequireSettled(); err != nil {
				return fmt.Errorf("completed response has an unresolved provider effect: %w", err)
			}
		}
		return nil
	case EnvelopeStatusError:
		if !r.errorSeen {
			return fmt.Errorf("error response completed without terminal error")
		}
		return nil
	default:
		return fmt.Errorf("response envelope status %q is unsupported", status)
	}
}

func (r *ValidatedResponseStream) requireOpenResponseEvent(event Event) error {
	if r.responseID == "" || r.assembler == nil {
		return fmt.Errorf("%s has no open response envelope", event.Kind)
	}
	if !r.identitySeen {
		return fmt.Errorf("%s arrived before response.identity", event.Kind)
	}
	if event.ParentID != "" || event.EnvID != "" && event.EnvID != r.responseID {
		return fmt.Errorf("%s envelope %q contradicts response %q", event.Kind, event.EnvID, r.responseID)
	}
	return nil
}
