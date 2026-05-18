package httpcodec

import "github.com/swobuforge/swobu/internal/domain/canonical"

// EnvelopeEventAdapter incrementally maps canonical envelope events to stream
// event primitives expected by existing family stream encoders.
type EnvelopeEventAdapter struct {
	started   bool
	resultID  string
	model     string
	itemKinds map[canonical.EnvelopeID]canonical.ItemKind
	itemIDs   map[canonical.EnvelopeID]string
	finish    string
	usage     canonical.TokenUsage
	completed bool
}

func NewEnvelopeEventAdapter() *EnvelopeEventAdapter {
	return &EnvelopeEventAdapter{
		itemKinds: map[canonical.EnvelopeID]canonical.ItemKind{},
		itemIDs:   map[canonical.EnvelopeID]string{},
	}
}

func (a *EnvelopeEventAdapter) Translate(ev canonical.Event) []StreamEvent {
	emitted := make([]StreamEvent, 0, 2)
	switch ev.Kind {
	case canonical.EventEnvelopeStart:
		a.translateEnvelopeStart(ev, &emitted)
	case canonical.EventMetadata:
		a.translateMetadata(ev)
	case canonical.EventTextDelta:
		a.translateTextDelta(ev, &emitted)
	case canonical.EventArgsDelta:
		a.translateArgsDelta(ev, &emitted)
	case canonical.EventEnvelopeEnd:
		a.translateEnvelopeEnd(ev, &emitted)
	case canonical.EventUsage:
		a.translateUsage(ev, &emitted)
	case canonical.EventFinish:
		a.translateFinish(ev, &emitted)
	case canonical.EventError:
		a.translateError(&emitted)
	}
	return emitted
}

func (a *EnvelopeEventAdapter) translateEnvelopeStart(ev canonical.Event, emitted *[]StreamEvent) {
	payload, _ := ev.Payload.(canonical.EnvelopeStartPayload)
	if payload.Kind == canonical.EnvResponse {
		if !a.started {
			a.started = true
			*emitted = append(*emitted, StreamEvent{Kind: StreamEventStarted})
		}
		return
	}
	if payload.Kind == canonical.EnvMessage {
		itemID := a.resolveItemID(ev)
		a.itemKinds[ev.EnvID] = canonical.ItemKindText
		a.itemIDs[ev.EnvID] = itemID
		*emitted = append(*emitted, StreamEvent{Kind: StreamEventItemStarted, ItemKind: canonical.ItemKindText, ItemID: itemID})
		return
	}
	if payload.Kind == canonical.EnvToolCall {
		itemID := a.resolveItemID(ev)
		a.itemKinds[ev.EnvID] = canonical.ItemKindToolUse
		a.itemIDs[ev.EnvID] = itemID
		*emitted = append(*emitted, StreamEvent{Kind: StreamEventItemStarted, ItemKind: canonical.ItemKindToolUse, ItemID: itemID, ToolUseID: payload.ToolUseID, Name: payload.Name})
	}
}

func (a *EnvelopeEventAdapter) translateMetadata(ev canonical.Event) {
	payload, _ := ev.Payload.(canonical.MetadataPayload)
	if id := payload.Values["result_id"]; id != "" {
		a.resultID = id
	}
	if model := payload.Values["model"]; model != "" {
		a.model = model
	}
}

func (a *EnvelopeEventAdapter) translateTextDelta(ev canonical.Event, emitted *[]StreamEvent) {
	payload, _ := ev.Payload.(canonical.TextDeltaPayload)
	*emitted = append(*emitted, StreamEvent{Kind: StreamEventTextDelta, ItemID: a.itemIDOrFallback(ev.EnvID), TextDelta: payload.Text})
}

func (a *EnvelopeEventAdapter) translateArgsDelta(ev canonical.Event, emitted *[]StreamEvent) {
	payload, _ := ev.Payload.(canonical.ArgsDeltaPayload)
	*emitted = append(*emitted, StreamEvent{Kind: StreamEventToolUseArgumentsDelta, ItemID: a.itemIDOrFallback(ev.EnvID), ArgumentsDelta: payload.Args})
}

func (a *EnvelopeEventAdapter) translateEnvelopeEnd(ev canonical.Event, emitted *[]StreamEvent) {
	payload, _ := ev.Payload.(canonical.EnvelopeEndPayload)
	if payload.Kind == canonical.EnvMessage || payload.Kind == canonical.EnvToolCall {
		itemID := a.itemIDOrFallback(ev.EnvID)
		itemKind := a.itemKinds[ev.EnvID]
		*emitted = append(*emitted, StreamEvent{Kind: StreamEventItemCompleted, ItemID: itemID, ItemKind: itemKind})
		delete(a.itemKinds, ev.EnvID)
		delete(a.itemIDs, ev.EnvID)
	}
	if payload.Kind == canonical.EnvResponse && !a.completed {
		*emitted = append(*emitted, StreamEvent{Kind: StreamEventCompleted, ResultID: a.resultID, Model: a.model, FinishReason: a.finish, Usage: a.usage})
		a.completed = true
	}
}

func (a *EnvelopeEventAdapter) translateUsage(ev canonical.Event, emitted *[]StreamEvent) {
	payload, _ := ev.Payload.(canonical.UsagePayload)
	a.usage = payload.Usage
	if !a.completed {
		*emitted = append(*emitted, StreamEvent{Kind: StreamEventCompleted, ResultID: a.resultID, Model: a.model, FinishReason: a.finish, Usage: a.usage})
		a.completed = true
	}
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

func (a *EnvelopeEventAdapter) translateError(emitted *[]StreamEvent) {
	if !a.completed {
		*emitted = append(*emitted, StreamEvent{Kind: StreamEventCompleted, ResultID: a.resultID, Model: a.model})
		a.completed = true
	}
}

func (a *EnvelopeEventAdapter) resolveItemID(ev canonical.Event) string {
	itemID := ev.Meta.NativeID
	if itemID == "" {
		itemID = string(ev.EnvID)
	}
	return itemID
}

func (a *EnvelopeEventAdapter) itemIDOrFallback(envID canonical.EnvelopeID) string {
	itemID := a.itemIDs[envID]
	if itemID == "" {
		itemID = string(envID)
	}
	return itemID
}
