package sse

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// EnvelopeEventAdapter incrementally maps canonical envelope events to stream
// event primitives expected by existing family stream encoders.
type EnvelopeEventAdapter struct {
	started      bool
	responseOpen bool
	resultID     string
	model        string
	itemKinds    map[uint32]canonical.ItemKind
	itemIDs      map[uint32]string
	finish       string
	usage        canonical.TokenUsage
	errorCode    string
	errorText    string
	completed    bool
}

func NewEnvelopeEventAdapter() *EnvelopeEventAdapter {
	return &EnvelopeEventAdapter{
		itemKinds: map[uint32]canonical.ItemKind{},
		itemIDs:   map[uint32]string{},
	}
}

func (a *EnvelopeEventAdapter) Translate(ev canonical.Event) ([]StreamEvent, error) {
	emitted := make([]StreamEvent, 0, 2)
	switch ev.Kind {
	case canonical.EventEnvelopeStart:
		a.translateEnvelopeStart(ev, &emitted)
	case canonical.EventResponseIdentity:
		if err := a.translateResponseIdentity(ev); err != nil {
			return nil, err
		}
		a.ensureStarted(&emitted)
	case canonical.EventItemStart:
		if a.resultID == "" {
			return nil, fmt.Errorf("item.start arrived before response.identity")
		}
		a.ensureStarted(&emitted)
		if err := a.translateItemStart(ev, &emitted); err != nil {
			return nil, err
		}
	case canonical.EventContentStart:
		if err := a.translateContentStart(ev, &emitted); err != nil {
			return nil, err
		}
	case canonical.EventTextDelta:
		if err := a.translateTextDelta(ev, &emitted); err != nil {
			return nil, err
		}
	case canonical.EventArgsDelta:
		if err := a.translateArgsDelta(ev, &emitted); err != nil {
			return nil, err
		}
	case canonical.EventItemCompleted:
		if err := a.translateItemCompleted(ev, &emitted); err != nil {
			return nil, err
		}
	case canonical.EventEnvelopeEnd:
		if a.responseOpen && a.resultID == "" {
			return nil, fmt.Errorf("response envelope ended without response.identity")
		}
		a.ensureStarted(&emitted)
		a.translateEnvelopeEnd(ev, &emitted)
	case canonical.EventUsage:
		a.translateUsage(ev)
	case canonical.EventFinish:
		a.translateFinish(ev, &emitted)
	case canonical.EventError:
		a.translateError(ev, &emitted)
	}
	return emitted, nil
}

func (a *EnvelopeEventAdapter) translateEnvelopeStart(ev canonical.Event, emitted *[]StreamEvent) {
	payload, _ := ev.Payload.(canonical.EnvelopeStartPayload)
	if payload.Kind == canonical.EnvResponse {
		a.model = payload.Model
		a.responseOpen = true
		return
	}
}

func (a *EnvelopeEventAdapter) ensureStarted(emitted *[]StreamEvent) {
	if !a.responseOpen || a.started {
		return
	}
	a.started = true
	*emitted = append(*emitted, StreamEvent{Kind: StreamEventStarted, ResultID: a.resultID, Model: a.model})
}

func (a *EnvelopeEventAdapter) translateItemStart(ev canonical.Event, emitted *[]StreamEvent) error {
	itemEvent, ok := ev.Payload.(canonical.ItemEvent)
	if !ok {
		return fmt.Errorf("item.start payload type %T is unsupported", ev.Payload)
	}
	start, ok := itemEvent.Payload.(canonical.ItemStartPayload)
	if !ok || start.Kind() == "" {
		return fmt.Errorf("item.start ordinal %d is invalid", itemEvent.Position.Item)
	}
	itemID := fmt.Sprintf("item_%d", itemEvent.Position.Item)
	a.itemKinds[itemEvent.Position.Item] = start.Kind()
	a.itemIDs[itemEvent.Position.Item] = itemID
	streamEvent := StreamEvent{Kind: StreamEventItemStarted, ItemKind: start.Kind(), ItemID: itemID, ItemOrdinal: itemEvent.Position.Item}
	if toolStart, ok := start.ToolCall(); ok {
		streamEvent.ToolUseID = toolStart.CallID.String()
		streamEvent.Name = toolStart.Tool.Name()
		streamEvent.ToolType = string(toolStart.Tool.Kind())
	}
	*emitted = append(*emitted, streamEvent)
	return nil
}

func (a *EnvelopeEventAdapter) translateResponseIdentity(ev canonical.Event) error {
	payload, ok := ev.Payload.(canonical.ResponseIdentityPayload)
	if !ok {
		return fmt.Errorf("response.identity payload type %T is unsupported", ev.Payload)
	}
	if a.resultID != "" {
		return fmt.Errorf("response.identity is duplicated")
	}
	resultID := payload.Response.SwobuID.String()
	if resultID == "" {
		return fmt.Errorf("response.identity requires a non-empty Swobu response ID")
	}
	a.resultID = resultID
	return nil
}

func (a *EnvelopeEventAdapter) translateContentStart(ev canonical.Event, emitted *[]StreamEvent) error {
	itemEvent, ok := ev.Payload.(canonical.ItemEvent)
	if !ok {
		return fmt.Errorf("content.start payload type %T is unsupported", ev.Payload)
	}
	payload, ok := itemEvent.Payload.(canonical.ContentStartPayload)
	if !ok {
		return fmt.Errorf("content.start item payload type %T is unsupported", itemEvent.Payload)
	}
	if payload.Kind == "" {
		return fmt.Errorf("content.start item payload has empty content kind")
	}
	*emitted = append(*emitted, StreamEvent{
		Kind:        StreamEventContentStarted,
		ItemID:      a.itemIDForOrdinal(itemEvent.Position.Item),
		ItemOrdinal: itemEvent.Position.Item,
		PartOrdinal: itemEvent.Position.Part,
		PartKind:    payload.Kind,
	})
	return nil
}

func (a *EnvelopeEventAdapter) translateTextDelta(ev canonical.Event, emitted *[]StreamEvent) error {
	itemEvent, ok := ev.Payload.(canonical.ItemEvent)
	if !ok {
		return fmt.Errorf("text.delta payload type %T is unsupported", ev.Payload)
	}
	payload, ok := itemEvent.Payload.(canonical.TextDeltaPayload)
	if !ok {
		return fmt.Errorf("text.delta item payload type %T is unsupported", itemEvent.Payload)
	}
	*emitted = append(*emitted, StreamEvent{Kind: StreamEventTextDelta, ItemID: a.itemIDForOrdinal(itemEvent.Position.Item), ItemOrdinal: itemEvent.Position.Item, PartOrdinal: itemEvent.Position.Part, TextDelta: payload.Text})
	return nil
}

func (a *EnvelopeEventAdapter) translateArgsDelta(ev canonical.Event, emitted *[]StreamEvent) error {
	itemEvent, ok := ev.Payload.(canonical.ItemEvent)
	if !ok {
		return fmt.Errorf("args.delta payload type %T is unsupported", ev.Payload)
	}
	payload, ok := itemEvent.Payload.(canonical.ArgsDeltaPayload)
	if !ok {
		return fmt.Errorf("args.delta item payload type %T is unsupported", itemEvent.Payload)
	}
	*emitted = append(*emitted, StreamEvent{Kind: StreamEventToolUseArgumentsDelta, ItemID: a.itemIDForOrdinal(itemEvent.Position.Item), ItemOrdinal: itemEvent.Position.Item, PartOrdinal: itemEvent.Position.Part, ArgumentsDelta: payload.Args})
	return nil
}

func (a *EnvelopeEventAdapter) translateItemCompleted(ev canonical.Event, emitted *[]StreamEvent) error {
	itemEvent, ok := ev.Payload.(canonical.ItemEvent)
	if !ok {
		return fmt.Errorf("item.completed payload type %T is unsupported", ev.Payload)
	}
	payload, ok := itemEvent.Payload.(canonical.ItemCompletedPayload)
	if !ok || payload.Item.Kind() == "" {
		return fmt.Errorf("item.completed ordinal %d is invalid", itemEvent.Position.Item)
	}
	itemID := a.itemIDForOrdinal(itemEvent.Position.Item)
	item := payload.Item.Clone()
	*emitted = append(*emitted, StreamEvent{Kind: StreamEventItemCompleted, ItemID: itemID, ItemKind: payload.Item.Kind(), ItemOrdinal: itemEvent.Position.Item, CompletedItem: &item})
	delete(a.itemKinds, itemEvent.Position.Item)
	delete(a.itemIDs, itemEvent.Position.Item)
	return nil
}

func (a *EnvelopeEventAdapter) translateEnvelopeEnd(ev canonical.Event, emitted *[]StreamEvent) {
	payload, _ := ev.Payload.(canonical.EnvelopeEndPayload)
	if payload.Kind == canonical.EnvResponse && !a.completed {
		if payload.Status == canonical.EnvelopeStatusError {
			*emitted = append(*emitted, a.failedEvent())
			a.completed = true
			return
		}
		*emitted = append(*emitted, StreamEvent{Kind: StreamEventCompleted, ResultID: a.resultID, Model: a.model, FinishReason: a.finish, Usage: a.usage})
		a.completed = true
	}
}

func (a *EnvelopeEventAdapter) translateUsage(ev canonical.Event) {
	payload, _ := ev.Payload.(canonical.UsagePayload)
	a.usage = payload.Usage
}

func (a *EnvelopeEventAdapter) translateFinish(ev canonical.Event, emitted *[]StreamEvent) {
	payload, _ := ev.Payload.(canonical.FinishPayload)
	a.finish = payload.Reason
	for i := len(*emitted) - 1; i >= 0; i-- {
		if (*emitted)[i].Kind == StreamEventCompleted {
			(*emitted)[i].FinishReason = a.finish
		}
	}
}

func (a *EnvelopeEventAdapter) translateError(ev canonical.Event, emitted *[]StreamEvent) {
	if payload, ok := ev.Payload.(canonical.ErrorPayload); ok {
		a.errorCode = payload.Code
		a.errorText = payload.Message
	}
	if !a.completed {
		*emitted = append(*emitted, a.failedEvent())
		a.completed = true
	}
}

func (a *EnvelopeEventAdapter) failedEvent() StreamEvent {
	code := a.errorCode
	if code == "" {
		code = "stream_error"
	}
	message := a.errorText
	if message == "" {
		message = "output stream failed"
	}
	return StreamEvent{
		Kind:         StreamEventFailed,
		ResultID:     a.resultID,
		Model:        a.model,
		ErrorCode:    code,
		ErrorMessage: message,
	}
}

func (a *EnvelopeEventAdapter) itemIDForOrdinal(ordinal uint32) string {
	itemID := a.itemIDs[ordinal]
	if itemID == "" {
		itemID = fmt.Sprintf("item_%d", ordinal)
	}
	return itemID
}
