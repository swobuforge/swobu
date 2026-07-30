package responses

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
)

func TestCanonicalResponsesReplayRetainsOnlyAdmittedBehavioralState(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"type":"message","id":"msg_1","status":"completed","phase":"final_answer","role":"assistant","content":"done","unknown_known_field":true},{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"brief"}],"encrypted_content":"cipher"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	document, err := EncodeCarrierWithChanges(
		EncodeInput{Request: decoded.Request.Request},
		delivery.BufferedDelivery(), nil, "", EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Input) != 2 {
		t.Fatalf("encoded input count = %d, want message and reasoning: %s", len(payload.Input), document.RawBytes())
	}
	for _, discarded := range [][]byte{
		[]byte(`"id":"msg_1"`),
		[]byte(`"id":"rs_1"`),
		[]byte(`"phase":"final_answer"`),
		[]byte(`unknown_known_field`),
	} {
		if bytes.Contains(document.RawBytes(), discarded) {
			t.Fatalf("unconsumed wire metadata %q survived: %s", discarded, document.RawBytes())
		}
	}
	if !bytes.Contains(payload.Input[1], []byte(`"encrypted_content":"cipher"`)) {
		t.Fatalf("continuation-consumed reasoning state was lost: %s", payload.Input[1])
	}
}

func TestCanonicalResponsesReplayPreservesCustomToolCallResultPair(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":"patch text"},{"type":"custom_tool_call_output","id":"ctco_1","call_id":"call_1","output":"Done"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 2 {
		t.Fatalf("canonical items len = %d, want 2", len(items))
	}
	call, ok := items[0].ToolCall()
	if !ok || call.Tool().Kind() != canonical.ToolKindCustom || call.CallID().String() != "call_1" {
		t.Fatalf("canonical call = %#v, want correlated custom call_1", call)
	}
	result, ok := items[1].ToolResult()
	if !ok || result.CallID() != call.CallID() {
		t.Fatalf("canonical result = %#v, want existing ToolResultItem correlated to call_1", result)
	}
	resultText, ok := result.Content()[0].Text()
	if !ok || resultText.Text() != "Done" {
		t.Fatalf("canonical result content = %#v, want exact text output", result.Content())
	}

	document, err := EncodeCarrierWithChanges(
		EncodeInput{Request: decoded.Request.Request},
		delivery.BufferedDelivery(), nil, "", EncodeOptions{},
	)
	if err != nil {
		t.Fatalf("EncodeCarrierWithChanges returned err=%v", err)
	}
	var payload struct {
		Input []struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Output any    `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatalf("decode replay document: %v", err)
	}
	if len(payload.Input) != 2 ||
		payload.Input[0].Type != "custom_tool_call" ||
		payload.Input[1].Type != "custom_tool_call_output" ||
		payload.Input[0].CallID != "call_1" ||
		payload.Input[1].CallID != "call_1" {
		t.Fatalf("replay pair = %#v, want correlated custom call/output", payload.Input)
	}
	if bytes.Contains(document.RawBytes(), []byte(`"id":"ctc_1"`)) ||
		bytes.Contains(document.RawBytes(), []byte(`"id":"ctco_1"`)) {
		t.Fatalf("presentation IDs entered canonical replay: %s", document.RawBytes())
	}

	changed, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, bytes.Replace(raw, []byte(`"output":"Done"`), []byte(`"output":"Changed"`), 1), carrier.Meta{}),
	)
	if err != nil {
		t.Fatalf("decode changed custom output: %v", err)
	}
	if changed.Request.RequestFingerprint == decoded.Request.RequestFingerprint {
		t.Fatal("custom output did not participate in the Responses history fingerprint")
	}

	segment, err := encodeConversation(decoded.Request.Request, items[1:], nil, mcp.Access{}, nil, "")
	if err != nil {
		t.Fatalf("encode result-only segment: %v", err)
	}
	segmentResult, ok := segment[0].(toolCallOutputItem)
	if !ok || segmentResult.Type != "custom_tool_call_output" {
		t.Fatalf("result-only segment = %#v, want custom output correlated from full history", segment)
	}
}

func TestCanonicalResponsesReplayPreservesEmptyCustomToolOutput(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"type":"custom_tool_call","call_id":"call_1","name":"shell","input":""},{"type":"custom_tool_call_output","call_id":"call_1","output":""}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}
	result, ok := decoded.Request.Request.Items()[1].ToolResult()
	if !ok {
		t.Fatal("custom output did not decode through ToolResultItem")
	}
	text, ok := result.Content()[0].Text()
	if !ok || text.Text() != "" {
		t.Fatalf("custom output text = %#v, want explicit empty text", result.Content())
	}

	document, err := EncodeCarrierWithChanges(
		EncodeInput{Request: decoded.Request.Request},
		delivery.BufferedDelivery(), nil, "", EncodeOptions{},
	)
	if err != nil {
		t.Fatalf("EncodeCarrierWithChanges returned err=%v", err)
	}
	var payload struct {
		Input []struct {
			Type   string  `json:"type"`
			Output *string `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatalf("decode replay document: %v", err)
	}
	if len(payload.Input) != 2 || payload.Input[1].Type != "custom_tool_call_output" ||
		payload.Input[1].Output == nil || *payload.Input[1].Output != "" {
		t.Fatalf("replay input = %#v, want present empty custom output", payload.Input)
	}
}

func TestDecodeClientRequest_CustomToolCallOutputPresence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
	}{
		{name: "absent"},
		{name: "null", output: `,"output":null`},
		{name: "non-string", output: `,"output":true`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"model":"m","input":[{"type":"custom_tool_call","call_id":"call_1","name":"shell","input":""},{"type":"custom_tool_call_output","call_id":"call_1"` + tc.output + `}]}`)
			if _, err := (ClientRequestDecoder{}).DecodeClientRequest(
				carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
			); err == nil {
				t.Fatalf("DecodeClientRequest accepted %s custom output", tc.name)
			}
		})
	}
}

func TestReplayableResponsesItemKindsHaveIngressAndReplayCoverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		wantKind     canonical.ItemKind
		wantToolKind canonical.ToolKind
	}{
		{name: "message", input: `{"type":"message","role":"assistant","content":"hello"}`, wantKind: canonical.ItemKindMessage},
		{name: "function call", input: `{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}`, wantKind: canonical.ItemKindToolCall, wantToolKind: canonical.ToolKindFunction},
		{name: "custom tool call", input: `{"type":"custom_tool_call","call_id":"call_1","name":"shell","input":""}`, wantKind: canonical.ItemKindToolCall, wantToolKind: canonical.ToolKindCustom},
		{name: "function call output", input: `{"type":"function_call_output","call_id":"call_1","output":"done"}`, wantKind: canonical.ItemKindToolResult},
		{name: "reasoning", input: `{"type":"reasoning","summary":[{"type":"summary_text","text":"brief"}],"encrypted_content":"cipher"}`, wantKind: canonical.ItemKindReasoning},
		{name: "tool search call", input: `{"type":"tool_search_call","call_id":"search_1","execution":"client","arguments":{"query":"files"}}`, wantKind: canonical.ItemKindToolCall, wantToolKind: canonical.ToolKindDiscovery},
		{name: "tool search call stringified arguments", input: `{"type":"tool_search_call","call_id":"search_1","execution":"client","arguments":"{\"query\":\"files\"}"}`, wantKind: canonical.ItemKindToolCall, wantToolKind: canonical.ToolKindDiscovery},
		{name: "tool search output", input: `{"type":"tool_search_output","call_id":"search_1","status":"completed","execution":"client","tools":[]}`, wantKind: canonical.ItemKindToolDiscoveryResult},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"model":"m","input":[` + tc.input + `]}`)
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
				carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
			)
			if err != nil {
				t.Fatalf("DecodeClientRequest returned err=%v", err)
			}
			items := decoded.Request.Request.Items()
			if len(items) != 1 || items[0].Kind() != tc.wantKind {
				t.Fatalf("canonical items = %#v, want one %q item", items, tc.wantKind)
			}
			if tc.wantToolKind != "" {
				call, ok := items[0].ToolCall()
				if !ok || call.Tool().Kind() != tc.wantToolKind {
					t.Fatalf("canonical tool call = %#v, want kind %q", call, tc.wantToolKind)
				}
			}

			document, err := EncodeCarrierWithChanges(
				EncodeInput{Request: decoded.Request.Request},
				delivery.BufferedDelivery(), nil, "", EncodeOptions{},
			)
			if err != nil {
				t.Fatalf("EncodeCarrierWithChanges returned err=%v", err)
			}
			var payload struct {
				Input []struct {
					Type string `json:"type"`
				} `json:"input"`
			}
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatalf("decode replay document: %v", err)
			}
			var source struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(tc.input), &source); err != nil {
				t.Fatalf("decode source item: %v", err)
			}
			if len(payload.Input) != 1 || payload.Input[0].Type != source.Type {
				t.Fatalf("replay item types = %#v, want %q", payload.Input, source.Type)
			}
		})
	}
}

func TestActionlessWebSearchMarkerPartitionsHistoryWithoutEnteringCanonical(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"type":"message","role":"user","content":"turn one"},{"type":"web_search_call","id":"ws_1","status":"completed"},{"type":"message","role":"user","content":"turn two"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Request.Request.Items()) != 2 {
		t.Fatalf("full canonical items = %#v, want only semantic messages", decoded.Request.Request.Items())
	}
	if decoded.Request.RebasedRequest == nil || len(decoded.Request.RebasedRequest.Request.Items()) != 1 {
		t.Fatalf("rebased request = %#v, want current message only", decoded.Request.RebasedRequest)
	}
}

func TestEncodeConversationPairsReusedFunctionAndCustomIDByOccurrence(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call_reused")
	functionKey, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	object, _ := canonical.ParseJSONObject([]byte(`{}`))
	functionCall, _ := canonical.NewToolCallItem(
		callID,
		functionKey,
		canonical.NewJSONObjectToolInput(object),
	)
	functionResult, _ := canonical.NewToolResultItem(
		callID,
		[]canonical.ToolResultPart{canonical.NewTextToolResultPart("function result")},
		false,
	)
	customKey, _ := canonical.NewRequestToolKey(canonical.ToolKindCustom, "shell")
	customCall, _ := canonical.NewToolCallItem(
		callID,
		customKey,
		canonical.NewTextToolInput("run"),
	)
	customResult, _ := canonical.NewToolResultItem(
		callID,
		[]canonical.ToolResultPart{canonical.NewTextToolResultPart("custom result")},
		false,
	)
	items := []canonical.CanonicalItem{functionCall, functionResult, customCall, customResult}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: items})

	encoded, err := encodeConversation(request, items, nil, mcp.Access{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 4 {
		t.Fatalf("encoded = %#v", encoded)
	}
	first, firstOK := encoded[1].(toolCallOutputItem)
	second, secondOK := encoded[3].(toolCallOutputItem)
	if !firstOK || first.Type != "function_call_output" ||
		!secondOK || second.Type != "custom_tool_call_output" {
		t.Fatalf("result output kinds = %#v, %#v", encoded[1], encoded[3])
	}
}
