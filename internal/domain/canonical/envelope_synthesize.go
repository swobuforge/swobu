package canonical

import (
	"fmt"
	"time"
)

// SynthesizeResponseEnvelopeEvents converts canonical response fields into a
// finite envelope event stream suitable for stream or batch adapters.
func SynthesizeResponseEnvelopeEvents(exchangeID string, response ResponseRef, model string, items []CanonicalItem, finishReason string, usage TokenUsage) []Event {
	seq := int64(0)
	next := func() int64 {
		seq++
		return seq
	}
	responseID := EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID))
	events := []Event{
		{
			ExchangeID: exchangeID,
			Seq:        next(),
			Time:       time.Now().UTC(),
			Kind:       EventEnvelopeStart,
			EnvID:      responseID,
			Payload: EnvelopeStartPayload{
				Kind: EnvResponse, Response: response.Clone(),
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        next(),
			Time:       time.Now().UTC(),
			Kind:       EventMetadata,
			EnvID:      responseID,
			Payload:    MetadataPayload{Values: map[string]string{"model": model}},
		},
	}
	msgIdx := 0
	toolIdx := 0
	for _, item := range items {
		switch item.Kind() {
		case ItemKindText:
			text, ok := item.TextItem()
			if !ok {
				continue
			}
			id := EnvelopeID(fmt.Sprintf("%s:message:%d", responseID, msgIdx))
			msgIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: responseID, Payload: EnvelopeStartPayload{Kind: EnvMessage, Role: item.Author()}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventTextDelta, EnvID: id, ParentID: responseID, Payload: TextDeltaPayload{Text: text.Text}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: responseID, Payload: EnvelopeEndPayload{Kind: EnvMessage, Status: EnvelopeStatusCompleted}},
			)
		case ItemKindToolUse:
			toolUse, ok := item.ToolUse()
			if !ok {
				continue
			}
			id := EnvelopeID(fmt.Sprintf("%s:tool_call:%d", responseID, toolIdx))
			toolIdx++
			args := toolUse.Input.RawObject()
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: responseID, Payload: EnvelopeStartPayload{Kind: EnvToolCall, Name: toolUse.Name, ToolUseID: toolUse.UseID, ToolType: toolUse.ToolType}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventArgsDelta, EnvID: id, ParentID: responseID, Payload: ArgsDeltaPayload{Args: args}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: responseID, Payload: EnvelopeEndPayload{Kind: EnvToolCall, Status: EnvelopeStatusCompleted}},
			)
		default:
			// Ignore unsupported output item kinds during synthesis.
		}
	}
	events = append(events,
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventUsage, EnvID: responseID, Payload: UsagePayload{Usage: usage}},
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventFinish, EnvID: responseID, Payload: FinishPayload{Reason: finishReason}},
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: responseID, Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}},
	)
	return events
}
