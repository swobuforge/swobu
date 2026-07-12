package canonical

import (
	"fmt"
	"time"
)

// SynthesizeResponseEnvelopeEvents converts canonical response fields into a
// finite envelope event stream suitable for stream or batch adapters.
func SynthesizeResponseEnvelopeEvents(exchangeID string, resultID string, model string, items []CanonicalItem, finishReason string, usage TokenUsage) []Event {
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
				Kind: EnvResponse,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        next(),
			Time:       time.Now().UTC(),
			Kind:       EventMetadata,
			EnvID:      responseID,
			Payload: MetadataPayload{Values: map[string]string{
				"result_id": resultID,
				"model":     model,
			}},
		},
	}
	msgIdx := 0
	toolIdx := 0
	for _, item := range items {
		switch item.Kind {
		case ItemKindText:
			id := EnvelopeID(fmt.Sprintf("%s:message:%d", responseID, msgIdx))
			msgIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: responseID, Payload: EnvelopeStartPayload{Kind: EnvMessage, Role: item.Author}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventTextDelta, EnvID: id, ParentID: responseID, Payload: TextDeltaPayload{Text: item.Text}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: responseID, Payload: EnvelopeEndPayload{Kind: EnvMessage, Status: EnvelopeStatusCompleted}},
			)
		case ItemKindToolUse:
			id := EnvelopeID(fmt.Sprintf("%s:tool_call:%d", responseID, toolIdx))
			toolIdx++
			args := item.Input.RawObject()
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: responseID, Payload: EnvelopeStartPayload{Kind: EnvToolCall, Name: item.Name, ToolUseID: item.ToolUseID, ToolType: item.ToolType}},
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
