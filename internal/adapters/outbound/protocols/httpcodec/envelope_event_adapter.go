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
		payload, _ := ev.Payload.(canonical.EnvelopeStartPayload)
		if payload.Kind == canonical.EnvResponse {
			if !a.started {
				a.started = true
				emitted = append(emitted, StreamEvent{Kind: StreamEventStarted})
			}
			return emitted
		}
		if payload.Kind == canonical.EnvMessage {
			a.itemKinds[ev.EnvID] = canonical.ItemKindText
			itemID := ev.Meta.NativeID
			if itemID == "" {
				itemID = string(ev.EnvID)
			}
			a.itemIDs[ev.EnvID] = itemID
			emitted = append(emitted, StreamEvent{Kind: StreamEventItemStarted, ItemKind: canonical.ItemKindText, ItemID: itemID})
		}
		if payload.Kind == canonical.EnvToolCall {
			a.itemKinds[ev.EnvID] = canonical.ItemKindToolUse
			itemID := ev.Meta.NativeID
			if itemID == "" {
				itemID = string(ev.EnvID)
			}
			a.itemIDs[ev.EnvID] = itemID
			emitted = append(emitted, StreamEvent{Kind: StreamEventItemStarted, ItemKind: canonical.ItemKindToolUse, ItemID: itemID, ToolUseID: payload.ToolUseID, Name: payload.Name})
		}
	case canonical.EventMetadata:
		payload, _ := ev.Payload.(canonical.MetadataPayload)
		if id := payload.Values["result_id"]; id != "" {
			a.resultID = id
		}
		if model := payload.Values["model"]; model != "" {
			a.model = model
		}
	case canonical.EventTextDelta:
		payload, _ := ev.Payload.(canonical.TextDeltaPayload)
		itemID := a.itemIDs[ev.EnvID]
		if itemID == "" {
			itemID = string(ev.EnvID)
		}
		emitted = append(emitted, StreamEvent{Kind: StreamEventTextDelta, ItemID: itemID, TextDelta: payload.Text})
	case canonical.EventArgsDelta:
		payload, _ := ev.Payload.(canonical.ArgsDeltaPayload)
		itemID := a.itemIDs[ev.EnvID]
		if itemID == "" {
			itemID = string(ev.EnvID)
		}
		emitted = append(emitted, StreamEvent{Kind: StreamEventToolUseArgumentsDelta, ItemID: itemID, ArgumentsDelta: payload.Args})
	case canonical.EventEnvelopeEnd:
		payload, _ := ev.Payload.(canonical.EnvelopeEndPayload)
		if payload.Kind == canonical.EnvMessage || payload.Kind == canonical.EnvToolCall {
			itemID := a.itemIDs[ev.EnvID]
			if itemID == "" {
				itemID = string(ev.EnvID)
			}
			itemKind := a.itemKinds[ev.EnvID]
			emitted = append(emitted, StreamEvent{Kind: StreamEventItemCompleted, ItemID: itemID, ItemKind: itemKind})
			delete(a.itemKinds, ev.EnvID)
			delete(a.itemIDs, ev.EnvID)
		}
		if payload.Kind == canonical.EnvResponse {
			if !a.completed {
				emitted = append(emitted, StreamEvent{Kind: StreamEventCompleted, ResultID: a.resultID, Model: a.model, FinishReason: a.finish, Usage: a.usage})
				a.completed = true
			}
		}
	case canonical.EventUsage:
		payload, _ := ev.Payload.(canonical.UsagePayload)
		a.usage = payload.Usage
		if !a.completed {
			emitted = append(emitted, StreamEvent{Kind: StreamEventCompleted, ResultID: a.resultID, Model: a.model, FinishReason: a.finish, Usage: a.usage})
			a.completed = true
		}
	case canonical.EventFinish:
		payload, _ := ev.Payload.(canonical.FinishPayload)
		a.finish = payload.Reason
		for i := len(emitted) - 1; i >= 0; i-- {
			if emitted[i].Kind == StreamEventCompleted {
				emitted[i].FinishReason = a.finish
			}
		}
	case canonical.EventError:
		if !a.completed {
			emitted = append(emitted, StreamEvent{Kind: StreamEventCompleted, ResultID: a.resultID, Model: a.model})
			a.completed = true
		}
	}
	return emitted
}
