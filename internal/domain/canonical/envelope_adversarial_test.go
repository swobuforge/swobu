package canonical

import (
	"context"
	"testing"
)

func TestGrammarValidator_AdversarialRejectsInvalidFlows(t *testing.T) {
	t.Run("delta without open envelope", func(t *testing.T) {
		v := NewGrammarValidator()
		err := v.Observe(Event{ExchangeID: "ex1", Seq: 1, Kind: EventTextDelta, EnvID: "missing", Payload: TextDeltaPayload{Text: "x"}})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("double close", func(t *testing.T) {
		v := NewGrammarValidator()
		start := Event{ExchangeID: "ex1", Seq: 1, Kind: EventEnvelopeStart, EnvID: "r1", Payload: EnvelopeStartPayload{Kind: EnvResponse}}
		end := Event{ExchangeID: "ex1", Seq: 2, Kind: EventEnvelopeEnd, EnvID: "r1", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}}
		if err := v.Observe(start); err != nil {
			t.Fatalf("start error: %v", err)
		}
		if err := v.Observe(end); err != nil {
			t.Fatalf("first end error: %v", err)
		}
		if err := v.Observe(Event{ExchangeID: "ex1", Seq: 3, Kind: EventEnvelopeEnd, EnvID: "r1", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}}); err == nil {
			t.Fatal("expected double-close error")
		}
	})

	t.Run("parent closes before child", func(t *testing.T) {
		v := NewGrammarValidator()
		mustNoErr(t, v.Observe(Event{ExchangeID: "ex1", Seq: 1, Kind: EventEnvelopeStart, EnvID: "r1", Payload: EnvelopeStartPayload{Kind: EnvResponse}}))
		mustNoErr(t, v.Observe(Event{ExchangeID: "ex1", Seq: 2, Kind: EventEnvelopeStart, EnvID: "m1", ParentID: "r1", Payload: EnvelopeStartPayload{Kind: EnvMessage, Role: ItemAuthorAssistant}}))
		if err := v.Observe(Event{ExchangeID: "ex1", Seq: 3, Kind: EventEnvelopeEnd, EnvID: "r1", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}}); err == nil {
			t.Fatal("expected parent-close-before-child error")
		}
	})

	t.Run("sequence regression", func(t *testing.T) {
		v := NewGrammarValidator()
		mustNoErr(t, v.Observe(Event{ExchangeID: "ex1", Seq: 10, Kind: EventEnvelopeStart, EnvID: "r1", Payload: EnvelopeStartPayload{Kind: EnvResponse}}))
		if err := v.Observe(Event{ExchangeID: "ex1", Seq: 9, Kind: EventEnvelopeEnd, EnvID: "r1", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}}); err == nil {
			t.Fatal("expected sequence error")
		}
	})
}

func TestEnvelopeSynthesizeProject_RoundTrip(t *testing.T) {
	inUsage, _ := NewTokenUsageWithOptional(intPtr(12), intPtr(7), nil, nil)
	out := NewConversationOutputWithUsage("resp_1", "gpt-x", []CanonicalItem{
		NewTextOutputItem("m1", "Hello"),
		NewToolUseOutputItem("t1", "tc_1", "search", map[string]any{"query": "swobu"}),
	}, "stop", inUsage)

	events, err := SynthesizeResponseFromOutput("ex_round", out)
	if err != nil {
		t.Fatalf("SynthesizeResponseFromOutput error: %v", err)
	}

	v := NewGrammarValidator()
	idx := NewEnvelopeIndex()
	for _, ev := range events {
		mustNoErr(t, v.Observe(ev))
		mustNoErr(t, idx.Observe(ev))
	}
	responseID := EnvelopeID("ex_round:response:0")
	projected, err := idx.ProjectResponse(responseID)
	if err != nil {
		t.Fatalf("ProjectResponse error: %v", err)
	}
	if projected.ResultID() != out.ResultID() {
		t.Fatalf("result id = %q, want %q", projected.ResultID(), out.ResultID())
	}
	if projected.Model() != out.Model() {
		t.Fatalf("model = %q, want %q", projected.Model(), out.Model())
	}
	if projected.Text() != "Hello" {
		t.Fatalf("text = %q, want %q", projected.Text(), "Hello")
	}
	if len(projected.Items()) != 2 {
		t.Fatalf("items len = %d, want 2", len(projected.Items()))
	}
	if projected.Items()[1].Kind != ItemKindToolUse {
		t.Fatalf("second item kind = %q, want %q", projected.Items()[1].Kind, ItemKindToolUse)
	}
	if got, ok := projected.Items()[1].Input["query"].(string); !ok || got != "swobu" {
		t.Fatalf("tool args query = %#v, want %q", projected.Items()[1].Input["query"], "swobu")
	}
}

func TestReadClosedEnvelope_Response(t *testing.T) {
	events := []Event{
		{ExchangeID: "ex1", Seq: 1, Kind: EventEnvelopeStart, EnvID: "r1", Payload: EnvelopeStartPayload{Kind: EnvResponse}},
		{ExchangeID: "ex1", Seq: 2, Kind: EventMetadata, EnvID: "r1", Payload: MetadataPayload{Values: map[string]string{"model": "gpt-y"}}},
		{ExchangeID: "ex1", Seq: 3, Kind: EventEnvelopeEnd, EnvID: "r1", Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}},
	}
	closed, err := ReadClosedEnvelope(context.Background(), NewSliceEventReader(events), EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope error: %v", err)
	}
	if closed.ID != "r1" {
		t.Fatalf("closed id = %q, want %q", closed.ID, "r1")
	}
}

func TestEnvelopeBuilder_AliasStability(t *testing.T) {
	b := NewEnvelopeBuilder("ex_alias")
	resp, err := b.Start(EnvResponse, "", EnvelopeStartPayload{})
	mustNoErr(t, err)
	key := AliasKey{Protocol: "openai.chat", Kind: "tool_call", NativeID: "abc", Index: 0}
	id1 := b.EnsureToolCall(resp.EnvID, key)
	id2 := b.EnsureToolCall(resp.EnvID, key)
	if id1 != id2 {
		t.Fatalf("alias ids differ: %q vs %q", id1, id2)
	}
}

func TestEnvelopeRequestSynthesizeProject_RoundTrip(t *testing.T) {
	in := NewGenerationRequest(GenerationRequestParams{
		Model: "gpt-r",
		Thread: []CanonicalItem{
			NewTextItem(ItemAuthorUser, "hello"),
			NewToolUseItem(ItemAuthorAssistant, "tool_0", "call_1", "search", map[string]any{"q": "swobu"}),
		},
		LastTurn: []CanonicalItem{
			NewTextItem(ItemAuthorUser, "hello"),
			NewToolUseItem(ItemAuthorAssistant, "tool_0", "call_1", "search", map[string]any{"q": "swobu"}),
		},
	})
	events, err := SynthesizeRequestFromCanonicalRequest("ex_req_rt", in)
	if err != nil {
		t.Fatalf("SynthesizeRequestFromCanonicalRequest error: %v", err)
	}
	v := NewGrammarValidator()
	idx := NewEnvelopeIndex()
	for _, ev := range events {
		mustNoErr(t, v.Observe(ev))
		mustNoErr(t, idx.Observe(ev))
	}
	closed, ok := idx.Closed("ex_req_rt:request:0")
	if !ok {
		t.Fatal("request envelope was not closed")
	}
	rebuilt, err := closed.ProjectRequest()
	if err != nil {
		t.Fatalf("ProjectRequest error: %v", err)
	}
	typed, ok := rebuilt.(GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("rebuilt type = %T, want GenerationCanonicalRequest", rebuilt)
	}
	if got := typed.Model(); got != "gpt-r" {
		t.Fatalf("model = %q, want %q", got, "gpt-r")
	}
	if len(typed.Thread()) != 2 {
		t.Fatalf("thread len = %d, want 2", len(typed.Thread()))
	}
	if got, ok := typed.Thread()[1].Input["q"].(string); !ok || got != "swobu" {
		t.Fatalf("tool input q = %#v, want %q", typed.Thread()[1].Input["q"], "swobu")
	}
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func intPtr(v int) *int { return &v }
