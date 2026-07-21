package responses

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestResponsesCaptureDropsClientSyntheticFunctionCallID(t *testing.T) {
	raw := []byte(`{"model":"gpt-4.1-mini","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}],"input":[{"type":"function_call","id":"item_0","call_id":"call_1","name":"search","arguments":"{}","future_member":true},{"type":"function_call","id":"fc_123","call_id":"call_2","name":"search","arguments":"{}"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}

	items := decoded.Request.ResponsesInput.JSONObjects()
	if len(items) != 2 {
		t.Fatalf("native input item count = %d, want 2", len(items))
	}
	var synthetic, providerOwned map[string]json.RawMessage
	if err := json.Unmarshal(items[0], &synthetic); err != nil {
		t.Fatal(err)
	}
	if _, found := synthetic["id"]; found {
		t.Fatalf("synthetic function-call id survived replay capture: %s", items[0])
	}
	if _, found := synthetic["future_member"]; !found {
		t.Fatalf("unknown member was not preserved: %s", items[0])
	}
	if err := json.Unmarshal(items[1], &providerOwned); err != nil {
		t.Fatal(err)
	}
	if string(providerOwned["id"]) != `"fc_123"` {
		t.Fatalf("provider function-call id was not preserved: %s", items[1])
	}
}

func TestDecodeClientRequest_AcceptsStringifiedFunctionCallArguments(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"gpt-4o-mini","previous_response_id":"swobu_resp_123","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}],"input":[{"type":"function_call","call_id":"call_1","name":"search","arguments":"{\"query\":\"hello\"}"}]}`)
	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}
	previous, ok := got.PreviousResponse()
	if !ok || previous.SwobuID != "swobu_resp_123" || previous.Responses != nil {
		t.Fatalf("client previous response = %#v, want Swobu-only selector", previous)
	}

	items := got.Items()
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].Kind() != canonical.ItemKindToolCall {
		t.Fatalf("items[0].Kind = %q, want %q", items[0].Kind(), canonical.ItemKindToolCall)
	}
	toolUse, _ := items[0].ToolCall()
	if got := toolUse.CallID().String(); got != "call_1" {
		t.Fatalf("items[0].ToolUseID = %q, want call_1", got)
	}
	if got := toolUse.Tool().String(); got == "" {
		t.Fatal("items[0] lost semantic tool identity")
	}
	object, _ := toolUse.Input().Object()
	if got := object.String(); got != `{"query":"hello"}` {
		t.Fatalf("items[0].Input.RawObject() = %q, want normalized object JSON", got)
	}
}

func TestDecodeClientRequest_AcceptsHistoricalFunctionCallWithoutCurrentTools(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"gpt-4o-mini","input":[{"type":"function_call","call_id":"call_1","name":"search","arguments":"{\"query\":\"hello\"}"}]}`)
	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}
	call, ok := got.Items()[0].ToolCall()
	if !ok || call.Tool().Namespace() != canonical.ToolNamespaceRequest || call.Tool().Name() != "search" {
		t.Fatalf("historical call tool = %#v, want request/function/search", call.Tool())
	}
}

func TestDecodeClientRequest_PreservesExplicitEmptyDurableBands(t *testing.T) {
	raw := []byte(`{
		"input":"continue",
		"previous_response_id":"swobu_resp_123",
		"model":"",
		"instructions":"",
		"tools":[],
		"tool_choice":null,
		"parallel_tool_calls":null,
		"text":null,
		"max_output_tokens":null,
		"stop":[],
		"temperature":null,
		"top_p":null
	}`)
	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !got.ModelSpecified() || !got.InstructionsSpecified() || !got.ToolsSpecified() || !got.ToolPolicySpecified() || !got.ToolCallBatchSpecified() || !got.OutputFormatSpecified() {
		t.Fatal("field-local durable-band presence was lost")
	}
	if canonicaltest.InstructionSetText(got.Instructions()) != "" || len(got.Tools()) != 0 || len(got.Controls().Limits.StopSequences) != 0 {
		t.Fatalf("explicit clears changed value: instructions=%q tools=%#v controls=%#v", canonicaltest.InstructionSetText(got.Instructions()), got.Tools(), got.Controls())
	}
}

func TestDecodeClientRequest_AcceptsMultilineToolOutputTranscript(t *testing.T) {
	t.Parallel()

	toolOutput := strings.Join([]string{
		`0017200   e   x   p   e   c   t   e   d       n   o   n   -   e   m   p`,
		`0017220   t   y       F   o   c   u   s   M   e   m   o   r   y`,
		`0017240  \n  \t   }  \n   }  \n  \n   /   /       -   -   -       S   i`,
		`0017760   r   y   .  \n  \t   /   /       T   h   i   s       t   e   s`,
		`0020140   r   i   n   g   s   .   T   o   U   p   p   e   r   (   "   "`,
	}, "\n")
	raw, err := json.Marshal(map[string]any{
		"model": "gpt-4o-mini",
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_1",
			"output":  toolOutput,
		}},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}
	items := got.Items()
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].Kind() != canonical.ItemKindToolResult {
		t.Fatalf("items[0].Kind = %q, want %q", items[0].Kind(), canonical.ItemKindToolResult)
	}
	toolResult, _ := items[0].ToolResult()
	if toolResult.CallID().String() != "call_1" {
		t.Fatalf("items[0] tool use ID = %q, want call_1", toolResult.CallID().String())
	}
	text, _ := toolResult.Content()[0].Text()
	if text.Text() != toolOutput {
		t.Fatalf("tool output changed during decode:\ngot:\n%s\nwant:\n%s", text.Text(), toolOutput)
	}
}

func TestDecodeClientRequest_RejectsNonJSONObjectFunctionCallArguments(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"gpt-4o-mini","previous_response_id":"swobu_resp_123","input":[{"type":"function_call","call_id":"call_1","name":"search","arguments":"oops"}]}`)
	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
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
	if !strings.Contains(compatErr.Message, "responses request function_call arguments are invalid") {
		t.Fatalf("error message = %q, want function_call arguments rejection", compatErr.Message)
	}
}
