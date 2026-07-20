package canonical

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestValidatedResponseStreamRejectsPrematureEOF(t *testing.T) {
	t.Parallel()
	start := Event{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}}
	identity := Event{Kind: EventResponseIdentity, EnvID: "response", Payload: ResponseIdentityPayload{Response: ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}}}
	finish := Event{Kind: EventFinish, EnvID: "response", Payload: FinishPayload{Reason: "stop"}}
	tests := map[string][]Event{
		"before response start": nil,
		"after response start":  {start},
		"after identity":        {start, identity},
		"with unfinished item": {
			start,
			identity,
			{Kind: EventItemStart, Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: testMessageStart(MessageRoleAssistant)}},
		},
		"after finish before response end": {start, identity, finish},
	}
	for name, events := range tests {
		t.Run(name, func(t *testing.T) {
			reader := NewValidatedResponseStream(NewSliceEventReader(events))
			for range events {
				if _, err := reader.Next(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := reader.Next(context.Background()); err == nil || errors.Is(err, io.EOF) {
				t.Fatalf("premature EOF error = %v, want lifecycle validation error", err)
			}
		})
	}
}

func TestValidatedResponseStreamAllowsEOFAfterCompletedEnvelope(t *testing.T) {
	t.Parallel()
	events := []Event{
		{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}},
		{Kind: EventResponseIdentity, EnvID: "response", Payload: ResponseIdentityPayload{Response: ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}}},
		{Kind: EventFinish, EnvID: "response", Payload: FinishPayload{Reason: "stop"}},
		{Kind: EventEnvelopeEnd, EnvID: "response", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}},
	}
	reader := NewValidatedResponseStream(NewSliceEventReader(events))
	for range events {
		if _, err := reader.Next(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reader.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("completed stream EOF = %v", err)
	}
}

func TestValidatedResponseStreamRejectsLifecycleViolationBeforeReturningEvent(t *testing.T) {
	events := []Event{
		{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}},
		{Kind: EventResponseIdentity, EnvID: "response", Payload: ResponseIdentityPayload{Response: ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}}},
		{Kind: EventTextDelta, EnvID: "part", Payload: ItemEvent{Position: ItemPosition{Item: 0, Part: 0}, Payload: TextDeltaPayload{Text: "escaped"}}},
	}
	reader := NewValidatedResponseStream(NewSliceEventReader(events))
	for index := 0; index < 2; index++ {
		if _, err := reader.Next(context.Background()); err != nil {
			t.Fatalf("event %d: %v", index, err)
		}
	}
	if _, err := reader.Next(context.Background()); err == nil {
		t.Fatal("text delta without item start escaped the shared validator")
	}
}

func TestValidatedResponseStreamRejectsDeltaAfterCompletion(t *testing.T) {
	message, _ := NewMessageItem(MessageRoleAssistant, []MessagePart{NewTextMessagePart("ok")})
	events := []Event{
		{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}},
		{Kind: EventResponseIdentity, EnvID: "response", Payload: ResponseIdentityPayload{Response: ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}}},
		{Kind: EventItemStart, Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: testMessageStart(MessageRoleAssistant)}},
		{Kind: EventContentStart, Payload: ItemEvent{Position: ItemPosition{Item: 0, Part: 0}, Payload: NewMessageContentStart(PartKindText)}},
		{Kind: EventTextDelta, Payload: ItemEvent{Position: ItemPosition{Item: 0, Part: 0}, Payload: TextDeltaPayload{Text: "ok"}}},
		{Kind: EventItemCompleted, Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: ItemCompletedPayload{Item: message}}},
		{Kind: EventTextDelta, Payload: ItemEvent{Position: ItemPosition{Item: 0, Part: 0}, Payload: TextDeltaPayload{Text: "late"}}},
	}
	reader := NewValidatedResponseStream(NewSliceEventReader(events))
	for index := 0; index < len(events)-1; index++ {
		if _, err := reader.Next(context.Background()); err != nil {
			t.Fatalf("event %d: %v", index, err)
		}
	}
	if _, err := reader.Next(context.Background()); err == nil {
		t.Fatal("text delta after completion escaped the shared validator")
	}
}

func TestValidatedResponseStreamEnforcesResponseIdentityOrdering(t *testing.T) {
	t.Parallel()
	identity := Event{Kind: EventResponseIdentity, EnvID: "response", Payload: ResponseIdentityPayload{Response: ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}}}
	tests := []struct {
		name   string
		events []Event
	}{
		{name: "item before identity", events: []Event{{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}}, {Kind: EventItemStart, Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: testMessageStart(MessageRoleAssistant)}}}},
		{name: "duplicate identity", events: []Event{{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}}, identity, identity}},
		{name: "empty identity", events: []Event{{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}}, {Kind: EventResponseIdentity, Payload: ResponseIdentityPayload{}}}},
		{name: "completion without identity", events: []Event{{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}}, {Kind: EventEnvelopeEnd, EnvID: "response", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader := NewValidatedResponseStream(NewSliceEventReader(tc.events))
			var err error
			for range tc.events {
				_, err = reader.Next(context.Background())
				if err != nil {
					break
				}
			}
			if err == nil {
				t.Fatal("identity violation escaped the shared validator")
			}
		})
	}
}

func TestValidatedResponseStreamRejectsContradictoryEnvelopeCoordinates(t *testing.T) {
	events := []Event{
		{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}},
		{Kind: EventResponseIdentity, EnvID: "response", Payload: ResponseIdentityPayload{Response: ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}}},
		{Kind: EventItemStart, EnvID: "item-0", ParentID: "another-response", Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: testMessageStart(MessageRoleAssistant)}},
	}
	reader := NewValidatedResponseStream(NewSliceEventReader(events))
	for index := 0; index < 2; index++ {
		if _, err := reader.Next(context.Background()); err != nil {
			t.Fatalf("event %d: %v", index, err)
		}
	}
	if _, err := reader.Next(context.Background()); err == nil {
		t.Fatal("contradictory item envelope ancestry was accepted")
	}
}

func TestValidatedResponseStreamRejectsSequenceRegressionAndDoubleClose(t *testing.T) {
	identity := ResponseIdentityPayload{Response: ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}}
	tests := [][]Event{
		{{Seq: 2, Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}}, {Seq: 1, Kind: EventResponseIdentity, EnvID: "response", Payload: identity}},
		{{Seq: 1, Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}}, {Seq: 2, Kind: EventResponseIdentity, EnvID: "response", Payload: identity}, {Seq: 3, Kind: EventEnvelopeEnd, EnvID: "response", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}}, {Seq: 4, Kind: EventEnvelopeEnd, EnvID: "response", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}}},
	}
	for _, events := range tests {
		reader := NewValidatedResponseStream(NewSliceEventReader(events))
		var err error
		for range events {
			_, err = reader.Next(context.Background())
			if err != nil {
				break
			}
		}
		if err == nil {
			t.Fatal("stream lifecycle violation was accepted")
		}
	}
}

func TestValidatedResponseStreamOwnsTerminalCardinalityAndExclusivity(t *testing.T) {
	start := Event{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}}
	identity := Event{Kind: EventResponseIdentity, EnvID: "response", Payload: ResponseIdentityPayload{Response: ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}}}
	finish := Event{Kind: EventFinish, EnvID: "response", Payload: FinishPayload{Reason: "stop"}}
	usage := Event{Kind: EventUsage, EnvID: "response", Payload: UsagePayload{Usage: NewUnknownTokenUsage()}}
	terminalError := Event{Kind: EventError, EnvID: "response", Payload: ErrorPayload{Code: "failed", Message: "failed"}}
	completed := Event{Kind: EventEnvelopeEnd, EnvID: "response", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}}
	tests := map[string][]Event{
		"duplicate finish":           {start, identity, finish, finish},
		"duplicate usage":            {start, identity, usage, usage},
		"error then finish":          {start, identity, terminalError, finish},
		"error then completed":       {start, identity, terminalError, completed},
		"completed without finish":   {start, identity, completed},
		"semantic event after close": {start, identity, finish, completed, usage},
	}
	for name, events := range tests {
		t.Run(name, func(t *testing.T) {
			reader := NewValidatedResponseStream(NewSliceEventReader(events))
			var err error
			for range events {
				_, err = reader.Next(context.Background())
				if err != nil {
					break
				}
			}
			if err == nil {
				t.Fatal("terminal grammar violation was accepted")
			}
		})
	}
}

func TestValidatedResponseStreamAllowsProtocolNeutralTerminalOrderings(t *testing.T) {
	start := Event{Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}}
	identity := Event{Kind: EventResponseIdentity, EnvID: "response", Payload: ResponseIdentityPayload{Response: ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}}}
	tests := map[string][]Event{
		"usage after finish": {
			start, identity,
			{Kind: EventFinish, EnvID: "response", Payload: FinishPayload{Reason: "stop"}},
			{Kind: EventUsage, EnvID: "response", Payload: UsagePayload{Usage: NewUnknownTokenUsage()}},
			{Kind: EventEnvelopeEnd, EnvID: "response", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}},
		},
		"terminal error": {
			start, identity,
			{Kind: EventError, EnvID: "response", Payload: ErrorPayload{Code: "failed", Message: "failed"}},
			{Kind: EventEnvelopeEnd, EnvID: "response", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusError}},
		},
	}
	for name, events := range tests {
		t.Run(name, func(t *testing.T) {
			reader := NewValidatedResponseStream(NewSliceEventReader(events))
			for range events {
				if _, err := reader.Next(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
