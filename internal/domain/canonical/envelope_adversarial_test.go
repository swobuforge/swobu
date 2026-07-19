package canonical

import (
	"context"
	"strings"
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
		NewToolUseOutputItem("t1", "tc_1", "search", NewToolArgumentsObject(`{"query":"swobu"}`)),
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
	if projected.Response().SwobuID != out.Response().SwobuID {
		t.Fatalf("response id = %q, want %q", projected.Response().SwobuID, out.Response().SwobuID)
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
	if projected.Items()[1].Kind() != ItemKindToolUse {
		t.Fatalf("second item kind = %q, want %q", projected.Items()[1].Kind(), ItemKindToolUse)
	}
	toolUse, _ := projected.Items()[1].ToolUse()
	if !strings.Contains(toolUse.Input.RawObject(), `"query":"swobu"`) {
		t.Fatalf("tool args query missing in %q", toolUse.Input.RawObject())
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

func TestEnvelopeRequestSynthesizeProject_RoundTrip(t *testing.T) {
	maxTokens := 96
	temperature := 0.3
	topP := 0.8
	controls, err := NewGenerationControls(GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
		StopSequences:   []string{"END"},
		Temperature:     &temperature,
		TopP:            &topP,
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	outputFormat, err := NewOutputFormat(OutputFormatParams{
		Kind:        OutputFormatJSONSchema,
		Name:        "request_shape",
		Description: "structured request shape",
		Schema:      NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`),
		Strict:      true,
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}
	in := NewCanonicalRequest(RequestParams{
		Model: "gpt-r",
		Items: []CanonicalItem{
			NewTextItem(ItemAuthorUser, "hello"),
			NewToolUseItem(ItemAuthorAssistant, "tool_0", "call_1", "search", NewToolArgumentsObject(`{"q":"swobu"}`)),
		},
		Tools: []ToolDecl{
			NewFunctionToolDecl("tool_1", "search", "search the workspace", NewToolSchemaObject(`{"type":"object","properties":{"q":{"type":"string"}}}`)),
		},
		ToolCallBatch: NewToolCallBatchPolicy(ToolCallBatchAtMostOne),
		ToolPolicy:    NewToolPolicy(ToolPolicyRequired, nil),
		Controls:      controls,
		OutputFormat:  outputFormat,
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
	typed := rebuilt
	if got := typed.Model(); got != "gpt-r" {
		t.Fatalf("model = %q, want %q", got, "gpt-r")
	}
	if len(typed.Items()) != 2 {
		t.Fatalf("thread len = %d, want 2", len(typed.Items()))
	}
	toolUse, _ := typed.Items()[1].ToolUse()
	if !strings.Contains(toolUse.Input.RawObject(), `"q":"swobu"`) {
		t.Fatalf("tool input q missing in %q", toolUse.Input.RawObject())
	}
	if len(typed.Tools()) != 1 {
		t.Fatalf("tools len = %d, want 1", len(typed.Tools()))
	}
	if got := typed.ToolPolicy(); got.Mode != ToolPolicyRequired {
		t.Fatalf("tool policy mode = %q, want %q", got.Mode, ToolPolicyRequired)
	}
	if got := typed.ToolCallBatch(); got.Mode != ToolCallBatchAtMostOne {
		t.Fatalf("tool call batch mode = %q, want %q", got.Mode, ToolCallBatchAtMostOne)
	}
	if got := typed.Tools()[0].(FunctionToolDecl); got.ToolName() != "search" || !strings.Contains(got.ToolInputSchema().RawObject(), `"q"`) {
		t.Fatalf("tool declaration roundtrip = %#v", got)
	}
	if got, ok := typed.Controls().Limits.MaxOutputTokens.Value(); !ok || got != 96 {
		t.Fatalf("controls max_output_tokens = (%d, %v), want (96, true)", got, ok)
	}
	if got := typed.Controls().Limits.StopSequences; len(got) != 1 || got[0] != "END" {
		t.Fatalf("controls stop sequences = %#v, want [END]", got)
	}
	if got, ok := typed.Controls().Sampling.Temperature.Value(); !ok || got != 0.3 {
		t.Fatalf("controls temperature = (%v, %v), want (0.3, true)", got, ok)
	}
	if got, ok := typed.Controls().Sampling.TopP.Value(); !ok || got != 0.8 {
		t.Fatalf("controls top_p = (%v, %v), want (0.8, true)", got, ok)
	}
	if gotFormat := typed.OutputFormat(); gotFormat.Kind != OutputFormatJSONSchema || gotFormat.Name != "request_shape" || gotFormat.Description != "structured request shape" || gotFormat.Schema.RawObject() != `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}` || !gotFormat.Strict {
		t.Fatalf("output format = %#v, want structured json schema", gotFormat)
	}
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func intPtr(v int) *int { return &v }
