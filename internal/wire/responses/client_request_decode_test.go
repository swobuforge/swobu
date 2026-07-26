package responses

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestResponsesKnownItemPresentationMetadataDoesNotEnterReplay(t *testing.T) {
	raw := []byte(`{"model":"gpt-4.1-mini","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}],"input":[{"type":"function_call","id":"item_0","call_id":"call_1","name":"search","arguments":"{}","future_member":true},{"type":"function_call","id":"fc_123","call_id":"call_2","name":"search","arguments":"{}"},{"type":"message","id":"item_1","role":"user","content":"hello"},{"type":"message","id":"msg_123","role":"assistant","status":"completed","content":"hi"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}

	items := decoded.Request.Request.Items()
	if len(items) != 4 {
		t.Fatalf("canonical item count = %d, want 4", len(items))
	}
	document, err := EncodeCarrierWithDecisions(
		EncodeInput{Request: decoded.Request.Request},
		delivery.BufferedDelivery(), nil, "", EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, presentationID := range []string{"item_0", "fc_123", "item_1", "msg_123"} {
		if strings.Contains(string(document.RawBytes()), presentationID) {
			t.Fatalf("presentation id %q survived replay: %s", presentationID, document.RawBytes())
		}
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

func TestDecodeClientRequest_AcceptsHistoricalCustomToolCall(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"gpt-4o-mini","input":[{"type":"custom_tool_call","id":"ctc_123","call_id":"call_1","name":"apply_patch","input":"line one\n\nline three\n"}]}`)
	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}
	items := got.Items()
	if len(items) != 1 {
		t.Fatalf("items len = %d, want one self-contained historical custom call", len(items))
	}
	call, ok := items[0].ToolCall()
	if !ok {
		t.Fatalf("items[0].Kind = %q, want %q", items[0].Kind(), canonical.ItemKindToolCall)
	}
	if call.CallID().String() != "call_1" || call.Tool().Kind() != canonical.ToolKindCustom || call.Tool().Name() != "apply_patch" {
		t.Fatalf("custom call identity = call_id:%q tool:%q, want call_1 and request/custom/apply_patch", call.CallID(), call.Tool())
	}
	input, ok := call.Input().Text()
	if !ok || input != "line one\n\nline three\n" {
		t.Fatalf("custom call input = %q, %t, want exact text input", input, ok)
	}

	encoded, err := EncodeCarrierWithDecisions(
		EncodeInput{Request: got},
		delivery.BufferedDelivery(), nil, "", EncodeOptions{},
	)
	if err != nil {
		t.Fatalf("EncodeCarrierWithDecisions returned err=%v", err)
	}
	for _, want := range []string{
		`"type":"custom_tool_call"`,
		`"call_id":"call_1"`,
		`"name":"apply_patch"`,
		`"input":"line one\n\nline three\n"`,
	} {
		if !strings.Contains(string(encoded.RawBytes()), want) {
			t.Fatalf("encoded custom call = %s, want %s", encoded.RawBytes(), want)
		}
	}
	if strings.Contains(string(encoded.RawBytes()), "ctc_123") {
		t.Fatalf("presentation id survived canonical replay: %s", encoded.RawBytes())
	}
}

func TestDecodeClientRequest_CustomToolCallInputPresence(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		input string
	}{
		{name: "omitted", input: ""},
		{name: "null", input: `,"input":null`},
		{name: "non-string", input: `,"input":42`},
	} {
		t.Run(tc.name+" is malformed", func(t *testing.T) {
			raw := []byte(`{"model":"gpt-4o-mini","input":[{"type":"custom_tool_call","call_id":"call_1","name":"shell"` + tc.input + `}]}`)
			_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
			var requestErr canonical.Error
			if !errors.As(err, &requestErr) || requestErr.Code != canonical.ErrorCodeBadRequest {
				t.Fatalf("DecodeClientRequest err = %v, want BAD_REQUEST", err)
			}
		})
	}

	t.Run("explicit empty is valid", func(t *testing.T) {
		raw := []byte(`{"model":"gpt-4o-mini","input":[{"type":"custom_tool_call","call_id":"call_1","name":"shell","input":""}]}`)
		got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
		if err != nil {
			t.Fatalf("DecodeClientRequest returned err=%v", err)
		}
		call, ok := got.Items()[0].ToolCall()
		if !ok {
			t.Fatalf("decoded item kind = %q, want %q", got.Items()[0].Kind(), canonical.ItemKindToolCall)
		}
		input, ok := call.Input().Text()
		if !ok || input != "" {
			t.Fatalf("custom call input = %q, %t, want present empty text", input, ok)
		}
	})
}

func TestDecodeClientRequest_NotImplementedNamesInputItemTypeAndPath(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"gpt-4o-mini","input":[{"type":"future_valid_item"}]}`)
	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("DecodeClientRequest err type = %T, want canonical.Error", err)
	}
	if compatErr.Code != canonical.ErrorCodeNotImplemented {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeNotImplemented)
	}
	for _, want := range []string{"/input/0/type", `"future_valid_item"`} {
		if !strings.Contains(compatErr.Message, want) {
			t.Fatalf("error message = %q, want %q", compatErr.Message, want)
		}
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
