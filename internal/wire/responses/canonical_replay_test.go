package responses

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
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
		EncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)},
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
		[]byte(`"phase":"final_answer"`),
		[]byte(`unknown_known_field`),
	} {
		if bytes.Contains(document.RawBytes(), discarded) {
			t.Fatalf("unconsumed wire metadata %q survived: %s", discarded, document.RawBytes())
		}
	}
	// RFC G2 §7.5: a reasoning id paired with encrypted_content is admitted — it
	// targets the same item on replay — while the message id stays erased.
	if !bytes.Contains(payload.Input[1], []byte(`"encrypted_content":"cipher"`)) {
		t.Fatalf("continuation-consumed reasoning state was lost: %s", payload.Input[1])
	}
	if !bytes.Contains(payload.Input[1], []byte(`"id":"rs_1"`)) {
		t.Fatalf("reasoning replay id was lost with its encrypted content: %s", payload.Input[1])
	}
	if !bytes.Contains(payload.Input[0], []byte(`"type":"output_text"`)) {
		t.Fatalf("assistant history did not use Responses output content grammar: %s", payload.Input[0])
	}
	if bytes.Contains(payload.Input[0], []byte(`"type":"input_text"`)) {
		t.Fatalf("assistant history used request input content grammar: %s", payload.Input[0])
	}
}

func TestResponsesRequestOmitsForeignOpaqueReasoningWithoutDroppingToolHistory(t *testing.T) {
	opaque, err := canonical.NewMessagesOpaqueThinking([]byte(`{"type":"thinking","thinking":"private","signature":"sig"}`))
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem(nil, opaque)
	if err != nil {
		t.Fatal(err)
	}
	key, err := canonical.NewRequestToolKey(canonical.ToolKindFunction, "search")
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	function, err := canonical.NewFunctionTool(key, "search", canonical.NewToolSchemaObject(schema), canonical.Unspecified[bool]())
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{function})
	declarations, _ := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	callID, _ := canonical.NewToolCallID("call_1")
	input, _ := canonical.ParseJSONObject([]byte(`{"q":"one"}`))
	call, err := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(input))
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("result")}, false)
	if err != nil {
		t.Fatal(err)
	}
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("continue")})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{declarations, reasoning, call, result, message},
	})

	var changes []compat.Change
	document, err := EncodeCarrierWithChanges(
		EncodeInput{Request: request, ToolNames: testAttemptToolNames(request)},
		delivery.BufferedDelivery(), &changes, "ex", EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Input) != 3 {
		t.Fatalf("encoded input=%#v want function call, output, and current message", payload.Input)
	}
	wantTypes := []string{"function_call", "function_call_output", "message"}
	for index, want := range wantTypes {
		if got, _ := payload.Input[index]["type"].(string); got != want {
			t.Fatalf("encoded input[%d].type=%q want %q; input=%#v", index, got, want, payload.Input)
		}
	}
	if payload.Input[0]["call_id"] != "call_1" || payload.Input[1]["call_id"] != "call_1" {
		t.Fatalf("tool correlation was lost: %#v", payload.Input)
	}
	if len(changes) != 1 || changes[0].Capability != canonical.RequestItemsKind || changes[0].Kind != compat.Omission {
		t.Fatalf("changes=%#v want one request-item omission", changes)
	}
	if item, ok := changes[0].Occurrence.RequestItem(); !ok || item != 1 {
		t.Fatalf("omission occurrence=%#v want request item 1", changes[0].Occurrence)
	}
}

func TestResponsesRequestRetainsOpaqueResponsesReasoningWithoutPortableParts(t *testing.T) {
	opaque, err := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{
		EncryptedContent: "cipher", ItemID: "rs_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem(nil, opaque)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{reasoning},
	})
	var changes []compat.Change
	document, err := EncodeCarrierWithChanges(
		EncodeInput{Request: request, ToolNames: testAttemptToolNames(request)},
		delivery.BufferedDelivery(), &changes, "ex", EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document.RawBytes(), []byte(`"type":"reasoning"`)) ||
		!bytes.Contains(document.RawBytes(), []byte(`"encrypted_content":"cipher"`)) ||
		!bytes.Contains(document.RawBytes(), []byte(`"id":"rs_1"`)) ||
		!bytes.Contains(document.RawBytes(), []byte(`"summary":[]`)) {
		t.Fatalf("Responses replay state was lost: %s", document.RawBytes())
	}
	if len(changes) != 0 {
		t.Fatalf("exact Responses replay recorded changes: %#v", changes)
	}
}

// RFC G2 §7.5: idless reasoning replay stays idless on encode; only an id paired
// with encrypted_content re-emerges, and message ids never do.
func TestCanonicalResponsesReplayKeepsReasoningIDPairedWithEncryptedContent(t *testing.T) {
	for _, table := range []struct {
		name    string
		input   string
		wantID  string
		wantEnc string
	}{
		{
			name:    "reasoning-id-paired-with-encrypted-content-survives",
			input:   `{"model":"m","input":[{"type":"reasoning","id":"rs_1","status":"completed","encrypted_content":"cipher","summary":[{"type":"summary_text","text":"brief"}]}]}`,
			wantID:  `"id":"rs_1"`,
			wantEnc: `"encrypted_content":"cipher"`,
		},
		{
			name:    "reasoning-without-encrypted-content-stays-idless",
			input:   `{"model":"m","input":[{"type":"reasoning","id":"rs_2","status":"completed","summary":[{"type":"summary_text","text":"brief"}]}]}`,
			wantID:  `"id":"rs_2"`,
			wantEnc: "",
		},
		{
			name:    "message-id-is-always-erased",
			input:   `{"model":"m","input":[{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":"done"}]}`,
			wantID:  `"id":"msg_1"`,
			wantEnc: "",
		},
	} {
		t.Run(table.name, func(t *testing.T) {
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
				carrier.NewDocument("", "application/json", nil, []byte(table.input), carrier.Meta{}),
			)
			if err != nil {
				t.Fatal(err)
			}
			document, err := EncodeCarrierWithChanges(
				EncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)},
				delivery.BufferedDelivery(), nil, "", EncodeOptions{},
			)
			if err != nil {
				t.Fatal(err)
			}
			hasID := bytes.Contains(document.RawBytes(), []byte(table.wantID))
			switch table.name {
			case "reasoning-id-paired-with-encrypted-content-survives":
				if !hasID {
					t.Fatalf("reasoning replay id %s was lost: %s", table.wantID, document.RawBytes())
				}
				if table.wantEnc != "" && !bytes.Contains(document.RawBytes(), []byte(table.wantEnc)) {
					t.Fatalf("encrypted content %s was lost: %s", table.wantEnc, document.RawBytes())
				}
			default:
				// idless reasoning and message ids must not survive encode.
				if hasID {
					t.Fatalf("id %s should have been erased: %s", table.wantID, document.RawBytes())
				}
			}
		})
	}
}

func TestCanonicalResponsesReplayPreservesCustomToolCallResultPair(t *testing.T) {
	raw := []byte(`{"model":"m","tools":[{"type":"custom","name":"apply_patch","format":{"type":"text"}}],"input":[{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":"patch text"},{"type":"custom_tool_call_output","id":"ctco_1","call_id":"call_1","output":"Done"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 3 {
		t.Fatalf("canonical items len = %d, want declaration plus call/result", len(items))
	}
	call, ok := items[1].ToolCall()
	if !ok || call.Tool().Kind() != canonical.ToolKindCustom || call.CallID().String() != "call_1" {
		t.Fatalf("canonical call = %#v, want correlated custom call_1", call)
	}
	result, ok := items[2].ToolResult()
	if !ok || result.CallID() != call.CallID() {
		t.Fatalf("canonical result = %#v, want existing ToolResultItem correlated to call_1", result)
	}
	resultText, ok := result.Content()[0].Text()
	if !ok || resultText.Text() != "Done" {
		t.Fatalf("canonical result content = %#v, want exact text output", result.Content())
	}

	document, err := EncodeCarrierWithChanges(
		EncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)},
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

	environment, err := canonical.EffectiveTools(decoded.Request.Request)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := compileResponsesToolProjection(environment.Declarations(), canonical.ToolVisibilityRefinements{}, testAttemptToolNames(decoded.Request.Request), nil, "", DefaultToolLowering())
	if err != nil {
		t.Fatal(err)
	}
	segment, err := encodeConversation(items[2:], decoded.Request.Request.Items(), nil, testAttemptToolNames(decoded.Request.Request), nil, nil, nil, "", &projection)
	if err != nil {
		t.Fatalf("encode result-only segment: %v", err)
	}
	segmentResult, ok := segment[0].(toolCallOutputItem)
	if !ok || segmentResult.Type != "custom_tool_call_output" {
		t.Fatalf("result-only segment = %#v, want custom output correlated from full history", segment)
	}
}

func TestCanonicalResponsesReplayPreservesEmptyCustomToolOutput(t *testing.T) {
	raw := []byte(`{"model":"m","tools":[{"type":"custom","name":"shell","format":{"type":"text"}}],"input":[{"type":"custom_tool_call","call_id":"call_1","name":"shell","input":""},{"type":"custom_tool_call_output","call_id":"call_1","output":""}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatalf("DecodeClientRequest returned err=%v", err)
	}
	result, ok := decoded.Request.Request.Items()[2].ToolResult()
	if !ok {
		t.Fatal("custom output did not decode through ToolResultItem")
	}
	text, ok := result.Content()[0].Text()
	if !ok || text.Text() != "" {
		t.Fatalf("custom output text = %#v, want explicit empty text", result.Content())
	}

	document, err := EncodeCarrierWithChanges(
		EncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)},
		delivery.BufferedDelivery(), nil, "", EncodeOptions{},
	)
	if err != nil {
		t.Fatalf("EncodeCarrierWithChanges returned err=%v", err)
	}
	var payload struct {
		Input []struct {
			Type   string `json:"type"`
			Output any    `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatalf("decode replay document: %v", err)
	}
	if len(payload.Input) != 2 || payload.Input[1].Type != "custom_tool_call_output" ||
		payload.Input[1].Output == nil {
		t.Fatalf("replay input = %#v, want present empty custom output", payload.Input)
	}
	if payload.Input[1].Output != "" {
		t.Fatalf("replay output = %#v, want explicit empty string", payload.Input[1].Output)
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
		tools        string
		wantKind     canonical.ItemKind
		wantToolKind canonical.ToolKind
		wantOmitted  bool
	}{
		{name: "message", input: `{"type":"message","role":"assistant","content":"hello"}`, wantKind: canonical.ItemKindMessage},
		{name: "function call", input: `{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}`, tools: `,"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]`, wantKind: canonical.ItemKindToolCall, wantToolKind: canonical.ToolKindFunction},
		{name: "custom tool call", input: `{"type":"custom_tool_call","call_id":"call_1","name":"shell","input":""}`, tools: `,"tools":[{"type":"custom","name":"shell","format":{"type":"text"}}]`, wantKind: canonical.ItemKindToolCall, wantToolKind: canonical.ToolKindCustom},
		{name: "function call output", input: `{"type":"function_call_output","call_id":"call_1","output":"done"}`, wantKind: canonical.ItemKindToolResult},
		{name: "reasoning", input: `{"type":"reasoning","summary":[{"type":"summary_text","text":"brief"}],"encrypted_content":"cipher"}`, wantKind: canonical.ItemKindReasoning},
		{name: "tool search call", input: `{"type":"tool_search_call","call_id":"search_1","execution":"client","arguments":{"query":"files"}}`, wantKind: canonical.ItemKindToolCall, wantToolKind: canonical.ToolKindDiscovery, wantOmitted: true},
		{name: "tool search call stringified arguments", input: `{"type":"tool_search_call","call_id":"search_1","execution":"client","arguments":"{\"query\":\"files\"}"}`, wantKind: canonical.ItemKindToolCall, wantToolKind: canonical.ToolKindDiscovery, wantOmitted: true},
		{name: "tool search output", input: `{"type":"tool_search_output","call_id":"search_1","status":"completed","execution":"client","tools":[]}`, wantKind: canonical.ItemKindToolDiscoveryResult, wantOmitted: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"model":"m"` + tc.tools + `,"input":[` + tc.input + `]}`)
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
				carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
			)
			if err != nil {
				t.Fatalf("DecodeClientRequest returned err=%v", err)
			}
			items := decoded.Request.Request.Items()
			current := items[len(items)-1]
			if current.Kind() != tc.wantKind {
				t.Fatalf("canonical items = %#v, want terminal %q item", items, tc.wantKind)
			}
			if tc.wantToolKind != "" {
				call, ok := current.ToolCall()
				if !ok || call.Tool().Kind() != tc.wantToolKind {
					t.Fatalf("canonical tool call = %#v, want kind %q", call, tc.wantToolKind)
				}
			}

			var changes []compat.Change
			document, err := EncodeCarrierWithChanges(
				EncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)},
				delivery.BufferedDelivery(), &changes, "", EncodeOptions{},
			)
			if tc.name == "function call output" {
				if err == nil || !strings.Contains(err.Error(), "tool result has no pending call") {
					t.Fatalf("result-only delta projection err=%v, want explicit missing provenance", err)
				}
				return
			}
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
			if tc.wantOmitted {
				if len(payload.Input) != 0 || !containsResponseChange(changes, canonical.RequestItemsKind, compat.Omission) {
					t.Fatalf("declaration-free replay input=%#v changes=%#v, want atomic omission", payload.Input, changes)
				}
				return
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
	declarations := canonicaltest.ToolDeclarations(t,
		canonicaltest.MustFunctionTool(functionKey, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]()),
		canonicaltest.MustCustomTool(customKey, "", canonical.NewToolFormatObject(canonicaltest.Object(t, `{"type":"text"}`))),
	)
	items := []canonical.CanonicalItem{declarations, functionCall, functionResult, customCall, customResult}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: items})

	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := compileResponsesToolProjection(environment.Declarations(), canonical.ToolVisibilityRefinements{}, testAttemptToolNames(request), nil, "", DefaultToolLowering())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeConversation(items, request.Items(), nil, testAttemptToolNames(request), nil, nil, nil, "", &projection)
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
