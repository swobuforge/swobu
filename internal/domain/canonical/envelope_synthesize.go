package canonical

import (
	"encoding/json"
	"fmt"
	"time"
)

// SynthesizeRequestFromCanonicalRequest converts a canonical request snapshot
// into a finite canonical envelope stream (batch as closed stream).
func SynthesizeRequestFromCanonicalRequest(exchangeID string, req CanonicalRequest) ([]Event, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	seq := int64(0)
	next := func() int64 {
		seq++
		return seq
	}
	requestID := EnvelopeID(fmt.Sprintf("%s:request:0", exchangeID))
	meta := map[string]string{
		"model": stringRequestModel(req),
	}
	switch req.SemanticKind() {
	case SemanticKindConversation:
		meta["semantic_kind"] = "conversation"
	case SemanticKindResponse:
		meta["semantic_kind"] = "response_generation"
	case SemanticKindPrompt:
		meta["semantic_kind"] = "prompt_generation"
	}
	events := []Event{
		{
			ExchangeID: exchangeID,
			Seq:        next(),
			Time:       time.Now().UTC(),
			Kind:       EventEnvelopeStart,
			EnvID:      requestID,
			Payload: EnvelopeStartPayload{
				Kind: EnvRequest,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        next(),
			Time:       time.Now().UTC(),
			Kind:       EventMetadata,
			EnvID:      requestID,
			Payload:    MetadataPayload{Values: meta},
		},
	}
	items := canonicalRequestItems(req)
	msgIdx := 0
	toolResultIdx := 0
	for _, item := range items {
		switch item.Kind {
		case ItemKindText:
			id := EnvelopeID(fmt.Sprintf("%s:message:%d", requestID, msgIdx))
			msgIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvMessage, Role: item.Author}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventTextDelta, EnvID: id, ParentID: requestID, Payload: TextDeltaPayload{Text: item.Text}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvMessage, Status: EnvelopeStatusCompleted}},
			)
		case ItemKindToolResult:
			id := EnvelopeID(fmt.Sprintf("%s:tool_result:%d", requestID, toolResultIdx))
			toolResultIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvToolResult, Role: item.Author, ToolUseID: item.ToolUseID}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventTextDelta, EnvID: id, ParentID: requestID, Payload: TextDeltaPayload{Text: item.Text}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvToolResult, Status: EnvelopeStatusCompleted}},
			)
		case ItemKindToolUse:
			id := EnvelopeID(fmt.Sprintf("%s:tool_result:%d", requestID, toolResultIdx))
			toolResultIdx++
			args := ""
			if item.Input != nil {
				// Encode as one args delta by policy; we do not fake token cadence.
				if b, err := json.Marshal(item.Input); err == nil {
					args = string(b)
				}
			}
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvToolResult, Role: item.Author, ToolUseID: item.ToolUseID, Name: item.Name}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventArgsDelta, EnvID: id, ParentID: requestID, Payload: ArgsDeltaPayload{Args: args}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvToolResult, Status: EnvelopeStatusCompleted}},
			)
		}
	}
	events = append(events,
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: requestID, Payload: EnvelopeEndPayload{Kind: EnvRequest, Status: EnvelopeStatusCompleted}},
	)
	return events, nil
}

// canonicalRequestItems normalizes request variants into an item sequence for
// envelope synthesis.
func canonicalRequestItems(req CanonicalRequest) []CanonicalItem {
	switch typed := req.(type) {
	case DialogCanonicalRequest:
		return typed.Items()
	case GenerationCanonicalRequest:
		return typed.Thread()
	case PromptCanonicalRequest:
		return []CanonicalItem{NewTextItem(ItemAuthorUser, typed.Prompt())}
	default:
		return nil
	}
}

// stringRequestModel extracts model identity without leaking request variant
// branching into synthesizer call sites.
func stringRequestModel(req CanonicalRequest) string {
	switch typed := req.(type) {
	case DialogCanonicalRequest:
		return typed.Model()
	case GenerationCanonicalRequest:
		return typed.Model()
	case PromptCanonicalRequest:
		return typed.Model()
	default:
		return ""
	}
}

// SynthesizeResponseFromOutput converts canonical output into a finite response
// envelope stream suitable for stream or batch adapters.
func SynthesizeResponseFromOutput(exchangeID string, output CanonicalOutput) ([]Event, error) {
	if output == nil {
		return nil, fmt.Errorf("output is nil")
	}
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
				"result_id": output.ResultID(),
				"model":     output.Model(),
			}},
		},
	}
	msgIdx := 0
	toolIdx := 0
	for _, item := range output.Items() {
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
			args := ""
			if item.Input != nil {
				// Emit semantic block-level args; transport framing decides cadence.
				if b, err := json.Marshal(item.Input); err == nil {
					args = string(b)
				}
			}
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: responseID, Payload: EnvelopeStartPayload{Kind: EnvToolCall, Name: item.Name, ToolUseID: item.ToolUseID}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventArgsDelta, EnvID: id, ParentID: responseID, Payload: ArgsDeltaPayload{Args: args}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: responseID, Payload: EnvelopeEndPayload{Kind: EnvToolCall, Status: EnvelopeStatusCompleted}},
			)
		}
	}
	events = append(events,
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventUsage, EnvID: responseID, Payload: UsagePayload{Usage: output.Usage()}},
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventFinish, EnvID: responseID, Payload: FinishPayload{Reason: output.FinishReason()}},
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: responseID, Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}},
	)
	return events, nil
}
