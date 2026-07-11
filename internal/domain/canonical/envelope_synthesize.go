package canonical

import (
	"fmt"
	"time"
)

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
			args := item.Input.RawObject()
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: responseID, Payload: EnvelopeStartPayload{Kind: EnvToolCall, Name: item.Name, ToolUseID: item.ToolUseID}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventArgsDelta, EnvID: id, ParentID: responseID, Payload: ArgsDeltaPayload{Args: args}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: responseID, Payload: EnvelopeEndPayload{Kind: EnvToolCall, Status: EnvelopeStatusCompleted}},
			)
		default:
			// Ignore unsupported output item kinds during synthesis.
		}
	}
	events = append(events,
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventUsage, EnvID: responseID, Payload: UsagePayload{Usage: output.Usage()}},
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventFinish, EnvID: responseID, Payload: FinishPayload{Reason: output.FinishReason()}},
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: responseID, Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}},
	)
	return events, nil
}

// SynthesizeRequestFromCanonicalRequest converts a canonical request into a
// finite request envelope stream suitable for round-trip projection tests.
func SynthesizeRequestFromCanonicalRequest(exchangeID string, request CanonicalRequest) ([]Event, error) {
	seq := int64(0)
	next := func() int64 {
		seq++
		return seq
	}
	requestID := EnvelopeID(fmt.Sprintf("%s:request:0", exchangeID))
	toolsRaw, err := encodeRequestToolDeclsMetadata(request.Tools())
	if err != nil {
		return nil, err
	}
	toolPolicyRaw, err := encodeToolPolicyMetadata(request.ToolPolicy())
	if err != nil {
		return nil, err
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
	}
	metadata := map[string]string{
		"model":       request.Model(),
		"tools":       toolsRaw,
		"tool_policy": toolPolicyRaw,
	}
	events = append(events, Event{
		ExchangeID: exchangeID,
		Seq:        next(),
		Time:       time.Now().UTC(),
		Kind:       EventMetadata,
		EnvID:      requestID,
		Payload:    MetadataPayload{Values: metadata},
	})
	msgIdx := 0
	toolIdx := 0
	resultIdx := 0
	for _, item := range request.Items() {
		switch item.Kind {
		case ItemKindText:
			id := EnvelopeID(fmt.Sprintf("%s:message:%d", requestID, msgIdx))
			msgIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvMessage, Role: item.Author}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventTextDelta, EnvID: id, ParentID: requestID, Payload: TextDeltaPayload{Text: item.Text}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvMessage, Status: EnvelopeStatusCompleted}},
			)
		case ItemKindToolUse:
			id := EnvelopeID(fmt.Sprintf("%s:tool_call:%d", requestID, toolIdx))
			toolIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvToolCall, Name: item.Name, ToolUseID: item.ToolUseID, Role: item.Author}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventArgsDelta, EnvID: id, ParentID: requestID, Payload: ArgsDeltaPayload{Args: item.Input.RawObject()}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvToolCall, Status: EnvelopeStatusCompleted}},
			)
		case ItemKindToolResult:
			id := EnvelopeID(fmt.Sprintf("%s:tool_result:%d", requestID, resultIdx))
			resultIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvToolResult, Name: item.Name, ToolUseID: item.ToolUseID, Role: item.Author}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventTextDelta, EnvID: id, ParentID: requestID, Payload: TextDeltaPayload{Text: item.Text}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvToolResult, Status: EnvelopeStatusCompleted}},
			)
		default:
			// Ignore unsupported request item kinds during synthesis.
		}
	}
	events = append(events,
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: requestID, Payload: EnvelopeEndPayload{Kind: EnvRequest, Status: EnvelopeStatusCompleted}},
	)
	return events, nil
}
