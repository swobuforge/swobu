package canonical

import "testing"

func TestConversationRequest_ClonesStructuredMessagesDeeply(t *testing.T) {
	maxTokens := 64
	temperature := 0.2
	topP := 0.9
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
		Name:        "structured_answer",
		Description: "return a structured answer",
		Schema:      NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`),
		Strict:      true,
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}
	req := NewCanonicalRequest(RequestParams{
		Model: "m",
		Items: []CanonicalItem{
			NewTextItem(ItemAuthorAssistant, "hi"),
			NewToolUseItem(ItemAuthorAssistant, "", "toolu_1", "calculator", NewToolArgumentsObject(`{"expr":"2+2"}`)),
		},
		ToolCallBatch: NewToolCallBatchPolicy(ToolCallBatchAtMostOne),
		Tools: []ToolDecl{
			NewFunctionToolDecl("tool_0", "calculator", "evaluate expressions", NewToolSchemaObject(`{"type":"object","properties":{"expr":{"type":"string"}}}`)),
			CustomToolDecl{
				ID:          NewSemanticToolID("apply_patch"),
				Name:        "apply_patch",
				Description: "Use the apply_patch tool to edit files.",
				Format:      NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`),
			},
		},
		Controls:     controls,
		OutputFormat: outputFormat,
	})

	cloned := req.Items()
	cloned[0].Text = "changed"
	cloned[1].Input = NewToolArgumentsObject(`{"expr":"changed"}`)
	tools := req.Tools()
	tool := tools[0].(FunctionToolDecl)
	tool.Name = "changed"
	tools[0] = tool
	custom := tools[1].(CustomToolDecl)
	custom.Name = "changed_custom"
	custom.Format = NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: changed"}`)
	tools[1] = custom
	clonedControls := req.Controls().Clone()
	clonedControls.Limits.StopSequences[0] = "mutated"

	got := req.Items()
	if got[0].Text != "hi" {
		t.Fatalf("text = %q, want %q", got[0].Text, "hi")
	}
	if got[1].Input.RawObject() != `{"expr":"2+2"}` {
		t.Fatalf("tool input = %q, want %q", got[1].Input.RawObject(), `{"expr":"2+2"}`)
	}
	if gotTool := req.Tools()[0].(FunctionToolDecl); gotTool.ToolName() != "calculator" {
		t.Fatalf("tool name = %q, want %q", gotTool.ToolName(), "calculator")
	}
	if gotCustom := req.Tools()[1].(CustomToolDecl); gotCustom.ToolName() != "apply_patch" {
		t.Fatalf("custom tool name = %q, want %q", gotCustom.ToolName(), "apply_patch")
	}
	if gotBatch := req.ToolCallBatch(); gotBatch.Mode != ToolCallBatchAtMostOne {
		t.Fatalf("tool call batch mode = %q, want %q", gotBatch.Mode, ToolCallBatchAtMostOne)
	}
	if gotMaxTokens, ok := req.Controls().Limits.MaxOutputTokens.Value(); !ok || gotMaxTokens != 64 {
		t.Fatalf("controls max_output_tokens = (%d, %v), want (64, true)", gotMaxTokens, ok)
	}
	if gotStop := req.Controls().Limits.StopSequences; len(gotStop) != 1 || gotStop[0] != "END" {
		t.Fatalf("controls stop_sequences = %#v, want [END]", gotStop)
	}
	if gotFormat := req.OutputFormat(); gotFormat.Kind != OutputFormatJSONSchema || gotFormat.Name != "structured_answer" || gotFormat.Description != "return a structured answer" || gotFormat.Schema.RawObject() != `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}` || !gotFormat.Strict {
		t.Fatalf("output format = %#v, want structured json schema", gotFormat)
	}
}

func TestResponseRequest_ClonesStructuredConversationStateDeeply(t *testing.T) {
	maxTokens := 128
	controls, err := NewGenerationControls(GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	req := NewCanonicalRequest(RequestParams{
		Model:         "m",
		Turn:          NewTurnRef("resp_123"),
		ToolCallBatch: NewToolCallBatchPolicy(ToolCallBatchAtMostOne),
		CacheIntent: NewCacheIntent(CacheIntentParams{
			Key: "repo-alpha",
		}),
		Items: []CanonicalItem{
			NewToolUseItem(ItemAuthorAssistant, "", "call_1", "grep", NewToolArgumentsObject(`{"pattern":"TODO"}`)),
		},
		Tools: []ToolDecl{
			NewFunctionToolDecl("tool_0", "grep", "search text", NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`)),
		},
		Controls: controls,
		OutputFormat: OutputFormat{
			Kind:        OutputFormatJSONSchema,
			Name:        "reply_schema",
			Description: "reply using a structured shape",
			Schema:      NewRawJSONObject(`{"type":"object","properties":{"result":{"type":"string"}},"required":["result"],"additionalProperties":false}`),
			Strict:      true,
		},
	})

	cloned := req.Clone()
	items := cloned.Items()
	items[0].Input = NewToolArgumentsObject(`{"pattern":"changed"}`)
	toolDecls := cloned.Tools()
	toolDecl := toolDecls[0].(FunctionToolDecl)
	toolDecl.Description = "changed"
	toolDecls[0] = toolDecl

	got := req.Items()
	if got[0].Input.RawObject() != `{"pattern":"TODO"}` {
		t.Fatalf("tool input = %q, want %q", got[0].Input.RawObject(), `{"pattern":"TODO"}`)
	}
	if gotTool := req.Tools()[0].(FunctionToolDecl); gotTool.ToolDescription() != "search text" {
		t.Fatalf("tool description = %q, want %q", gotTool.ToolDescription(), "search text")
	}
	if gotBatch := req.ToolCallBatch(); gotBatch.Mode != ToolCallBatchAtMostOne {
		t.Fatalf("tool call batch mode = %q, want %q", gotBatch.Mode, ToolCallBatchAtMostOne)
	}
	if prev, ok := cloned.Turn().PreviousID(); !ok || prev.String() != "resp_123" || cloned.CacheIntent().Key() != "repo-alpha" {
		t.Fatalf("clone lost response state")
	}
	if gotBatch := cloned.ToolCallBatch(); gotBatch.Mode != ToolCallBatchAtMostOne {
		t.Fatalf("clone tool call batch mode = %q, want %q", gotBatch.Mode, ToolCallBatchAtMostOne)
	}
	if gotMaxTokens, ok := cloned.Controls().Limits.MaxOutputTokens.Value(); !ok || gotMaxTokens != 128 {
		t.Fatalf("clone controls max_output_tokens = (%d, %v), want (128, true)", gotMaxTokens, ok)
	}
	if gotFormat := cloned.OutputFormat(); gotFormat.Kind != OutputFormatJSONSchema || gotFormat.Name != "reply_schema" || gotFormat.Description != "reply using a structured shape" || gotFormat.Schema.RawObject() != `{"type":"object","properties":{"result":{"type":"string"}},"required":["result"],"additionalProperties":false}` || !gotFormat.Strict {
		t.Fatalf("clone output format = %#v, want structured json schema", gotFormat)
	}
}
