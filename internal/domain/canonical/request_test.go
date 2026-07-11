package canonical

import "testing"

func TestConversationRequest_ClonesStructuredMessagesDeeply(t *testing.T) {
	req := NewCanonicalRequest(RequestParams{
		Model: "m",
		Items: []CanonicalItem{
			NewTextItem(ItemAuthorAssistant, "hi"),
			NewToolUseItem(ItemAuthorAssistant, "", "toolu_1", "calculator", NewToolArgumentsObject(`{"expr":"2+2"}`)),
		},
		Tools: []ToolDecl{
			NewFunctionToolDecl("tool_0", "calculator", "evaluate expressions", NewToolSchemaObject(`{"type":"object","properties":{"expr":{"type":"string"}}}`)),
		},
	})

	cloned := req.Items()
	cloned[0].Text = "changed"
	cloned[1].Input = NewToolArgumentsObject(`{"expr":"changed"}`)
	tools := req.Tools()
	tool := tools[0].(FunctionToolDecl)
	tool.Name = "changed"
	tools[0] = tool

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
}

func TestResponseRequest_ClonesStructuredConversationStateDeeply(t *testing.T) {
	req := NewCanonicalRequest(RequestParams{
		Model: "m",
		Turn:  NewTurnRef("resp_123"),
		CacheIntent: NewCacheIntent(CacheIntentParams{
			Key: "repo-alpha",
		}),
		Items: []CanonicalItem{
			NewToolUseItem(ItemAuthorAssistant, "", "call_1", "grep", NewToolArgumentsObject(`{"pattern":"TODO"}`)),
		},
		Tools: []ToolDecl{
			NewFunctionToolDecl("tool_0", "grep", "search text", NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`)),
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
	if prev, ok := cloned.Turn().PreviousID(); !ok || prev.String() != "resp_123" || cloned.CacheIntent().Key() != "repo-alpha" {
		t.Fatalf("clone lost response state")
	}
}
