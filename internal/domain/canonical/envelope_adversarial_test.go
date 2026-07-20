package canonical

import (
	"context"
	"reflect"
	"testing"
)

func TestSynthesizeAndProjectReasoningIsIdentity(t *testing.T) {
	block, err := NewMessagesOpaqueThinking([]byte(`{"type":"thinking","thinking":"summary","signature":"signature"}`))
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := NewReasoningItem([]ReasoningPart{mustReasoningPart(t, ReasoningPartSummary, "summary")}, block)
	if err != nil {
		t.Fatal(err)
	}
	response := ResponseRef{SwobuID: NewSwobuResponseID("resp_reasoning")}
	events := SynthesizeResponseEnvelopeEvents("ex", response, "model", []CanonicalItem{reasoning}, "stop", NewUnknownTokenUsage())
	closed := &ClosedEnvelope{Kind: EnvResponse, Events: events}
	projected, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if got := projected.Items(); !reflect.DeepEqual(got, []CanonicalItem{reasoning}) {
		t.Fatalf("projected items differ from buffered truth: %#v", got)
	}
}

func TestSynthesizedResponseProjectsOnlyCompletedCheckpoints(t *testing.T) {
	message, _ := NewMessageItem(MessageRoleAssistant, []MessagePart{NewTextMessagePart("hello")})
	output, err := NewCanonicalResponse(ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}, "model", []CanonicalItem{message}, "stop", NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	events := SynthesizeResponseEnvelopeEvents("ex", output.Response(), output.Model(), output.Items(), output.CompletionReason(), output.Usage())
	closed, err := ReadClosedEnvelope(context.Background(), NewSliceEventReader(events), EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if projected.Model() != "model" || projected.Response().SwobuID.String() != "resp_1" || len(projected.Items()) != 1 {
		t.Fatal("response projection lost typed checkpoint data")
	}
}

func TestResponseProjectionRejectsIncompleteStartedItem(t *testing.T) {
	events := []Event{
		{Seq: 1, Kind: EventEnvelopeStart, EnvID: "response", Payload: EnvelopeStartPayload{Kind: EnvResponse}},
		{Seq: 2, Kind: EventItemStart, EnvID: "item", ParentID: "response", Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: testMessageStart(MessageRoleAssistant)}},
		{Seq: 3, Kind: EventEnvelopeEnd, EnvID: "response", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}},
	}
	closed, err := ReadClosedEnvelope(context.Background(), NewSliceEventReader(events), EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.ProjectResponse(); err == nil {
		t.Fatal("projection accepted item without completed checkpoint")
	}
}

func TestResponseProjectionRejectsRequestOnlyItems(t *testing.T) {
	userMessage, _ := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart("not model output")})
	events := SynthesizeResponseEnvelopeEvents("ex", ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}, "model", []CanonicalItem{userMessage}, "stop", NewUnknownTokenUsage())
	closed, err := ReadClosedEnvelope(context.Background(), NewSliceEventReader(events), EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.ProjectResponse(); err == nil {
		t.Fatal("response projection accepted a user-authored message")
	}
}

func TestReadClosedEnvelopeRejectsConcurrentResponsesInsteadOfBroadcastingItems(t *testing.T) {
	events := []Event{
		{Seq: 1, Kind: EventEnvelopeStart, EnvID: "response-a", Payload: EnvelopeStartPayload{Kind: EnvResponse}},
		{Seq: 2, Kind: EventEnvelopeStart, EnvID: "response-b", Payload: EnvelopeStartPayload{Kind: EnvResponse}},
	}
	if _, err := ReadClosedEnvelope(context.Background(), NewSliceEventReader(events), EnvResponse); err == nil {
		t.Fatal("concurrent response envelopes were accepted")
	}
}
