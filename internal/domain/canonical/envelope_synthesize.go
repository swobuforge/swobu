package canonical

import (
	"fmt"
	"time"
)

// SynthesizeResponseEnvelopeEvents converts one buffered canonical response
// into progressive delivery evidence plus one authoritative completed snapshot
// per item.
func SynthesizeResponseEnvelopeEvents(exchangeID string, response ResponseRef, model string, items []CanonicalItem, finishReason string, usage TokenUsage) []Event {
	seq := int64(0)
	next := func(kind EventKind, envID, parentID EnvelopeID, payload any) Event {
		seq++
		return Event{ExchangeID: exchangeID, Seq: seq, Time: time.Now().UTC(), Kind: kind, EnvID: envID, ParentID: parentID, Payload: payload}
	}
	responseID := EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID))
	events := []Event{
		next(EventEnvelopeStart, responseID, "", EnvelopeStartPayload{Kind: EnvResponse, Model: model}),
		next(EventResponseIdentity, responseID, "", ResponseIdentityPayload{Response: response.Clone()}),
	}
	for index, original := range items {
		item := original.Clone()
		ordinal := uint32(index)
		switch item.Kind() {
		case ItemKindMessage:
			message, _ := item.Message()
			events = append(events, next(EventItemStart, "", "", ItemEvent{Position: ItemPosition{Item: ordinal}, Payload: messageStartFromValidatedItem(message.Role())}))
			for partIndex, part := range message.Content() {
				events = append(events, next(EventContentStart, "", "", ItemEvent{Position: ItemPosition{Item: ordinal, Part: uint32(partIndex)}, Payload: ContentStartPayload{Kind: part.Kind()}}))
				if text, ok := part.Text(); ok {
					events = append(events, next(EventTextDelta, "", "", ItemEvent{Position: ItemPosition{Item: ordinal, Part: uint32(partIndex)}, Payload: TextDeltaPayload{Text: text.Text()}}))
				}
			}
		case ItemKindToolCall:
			call, _ := item.ToolCall()
			events = append(events, next(EventItemStart, "", "", ItemEvent{Position: ItemPosition{Item: ordinal}, Payload: toolCallStartFromValidatedItem(call.CallID(), call.Tool())}))
			if object, ok := call.Input().Object(); ok {
				events = append(events, next(EventArgsDelta, "", "", ItemEvent{Position: ItemPosition{Item: ordinal}, Payload: ArgsDeltaPayload{Args: object.String()}}))
			} else if text, ok := call.Input().Text(); ok {
				events = append(events, next(EventArgsDelta, "", "", ItemEvent{Position: ItemPosition{Item: ordinal}, Payload: ArgsDeltaPayload{Args: text}}))
			}
		case ItemKindToolResult:
			// Tool results have no progressive start contract in this RFC. They
			// cross this synthesized stream only as an atomic completed checkpoint.
		default:
			events = append(events, next(EventError, responseID, "", ErrorPayload{Code: "invalid_canonical_item", Message: "canonical response contains an invalid item"}))
			continue
		}
		events = append(events, next(EventItemCompleted, "", "", ItemEvent{Position: ItemPosition{Item: ordinal}, Payload: ItemCompletedPayload{Item: item}}))
	}
	events = append(events,
		next(EventUsage, responseID, "", UsagePayload{Usage: usage}),
		next(EventFinish, responseID, "", FinishPayload{Reason: finishReason}),
		next(EventEnvelopeEnd, responseID, "", EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}),
	)
	return events
}
