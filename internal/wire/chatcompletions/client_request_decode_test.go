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

	raw := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","tool_calls":[{"type":"function","id":"tc_1","function":{"name":"search","arguments":"{\"query\":\"hello\"}"}}]}]}`)
	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: raw})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}

	items := got.Items()
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].Kind() != canonical.ItemKindToolUse {
		t.Fatalf("items[0].Kind = %q, want %q", items[0].Kind(), canonical.ItemKindToolUse)
	}
	toolUse, _ := items[0].ToolUse()
	if got := toolUse.UseID; got != "tc_1" {
		t.Fatalf("items[0].ToolUseID = %q, want tc_1", got)
	}
	if got := toolUse.Name; got != "search" {
		t.Fatalf("items[0].Name = %q, want search", got)
	}
	if got := toolUse.Input.RawObject(); got != `{"query":"hello"}` {
		t.Fatalf("items[0].Input.RawObject() = %q, want normalized object JSON", got)
	}
}

func TestDecodeClientRequest_RejectsNonJSONObjectFunctionCallArguments(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","tool_calls":[{"type":"function","id":"tc_1","function":{"name":"search","arguments":"oops"}}]}]}`)
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
	if got.Instructions() != "You are a coding agent.\n\nUse native tools for file edits." {
		t.Fatalf("instructions = %q", got.Instructions())
	}
	items := got.Items()
	if len(items) != 1 {
		t.Fatalf("items len = %d, want only user message", len(items))
	}
	text, _ := items[0].TextItem()
	if items[0].Author() != canonical.ItemAuthorUser || text.Text != "inspect files" {
		t.Fatalf("item = %#v, want user inspect files", items[0])
	}
}
