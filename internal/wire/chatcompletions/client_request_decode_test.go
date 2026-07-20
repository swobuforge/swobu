package chatcompletions

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeClientRequest_AcceptsStringifiedFunctionCallArguments(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"gpt-4o-mini","tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object"}}}],"messages":[{"role":"user","tool_calls":[{"type":"function","id":"tc_1","function":{"name":"search","arguments":"{\"query\":\"hello\"}"}}]}]}`)
	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: raw})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}

	items := got.Items()
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].Kind() != canonical.ItemKindToolCall {
		t.Fatalf("items[0].Kind = %q, want %q", items[0].Kind(), canonical.ItemKindToolCall)
	}
	toolUse, _ := items[0].ToolCall()
	if got := toolUse.CallID().String(); got != "tc_1" {
		t.Fatalf("items[0].ToolUseID = %q, want tc_1", got)
	}
	object, _ := toolUse.Input().Object()
	if got := object.String(); got != `{"query":"hello"}` {
		t.Fatalf("items[0].Input.RawObject() = %q, want normalized object JSON", got)
	}
}

func TestDecodeClientRequest_AcceptsHistoricalToolCallsWithoutCurrentTools(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"assistant","tool_calls":[{"type":"function","id":"tc_1","function":{"name":"search","arguments":{"q":"hello"}}}]}]}`)
	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: raw})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}
	call, ok := got.Items()[0].ToolCall()
	if !ok || call.Tool().Namespace() != canonical.ToolNamespaceRequest || call.Tool().Name() != "search" {
		t.Fatalf("historical call tool = %#v, want request/function/search", call.Tool())
	}
}

func TestDecodeClientRequest_RejectsNonJSONObjectFunctionCallArguments(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"gpt-4o-mini","tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object"}}}],"messages":[{"role":"user","tool_calls":[{"type":"function","id":"tc_1","function":{"name":"search","arguments":"oops"}}]}]}`)
	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: raw})
	if err == nil {
		t.Fatal("DecodeClientRequest returned nil error, want BAD_REQUEST")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("DecodeClientRequest err type = %T, want canonical.Error", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
	if !strings.Contains(compatErr.Message, "chat completions tool call arguments are invalid") {
		t.Fatalf("error message = %q, want function_call arguments rejection", compatErr.Message)
	}
}

func TestDecodeClientRequest_PreservesSystemAndDeveloperMessagesAsInstructions(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"gpt-4o-mini","messages":[
		{"role":"system","content":"You are a coding agent."},
		{"role":"developer","content":"Use native tools for file edits."},
		{"role":"user","content":"inspect files"}
	]}`)
	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: raw})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}
	instructions := got.Instructions().Instructions()
	if len(instructions) != 2 || instructions[0].Role() != canonical.MessageRoleSystem || instructions[0].Text() != "You are a coding agent." || instructions[1].Role() != canonical.MessageRoleDeveloper || instructions[1].Text() != "Use native tools for file edits." {
		t.Fatalf("instructions = %#v", instructions)
	}
	items := got.Items()
	if len(items) != 1 {
		t.Fatalf("items len = %d, want only user message", len(items))
	}
	message, _ := items[0].Message()
	text, _ := message.Content()[0].Text()
	if message.Role() != canonical.MessageRoleUser || text.Text() != "inspect files" {
		t.Fatalf("item = %#v, want user inspect files", items[0])
	}
}
