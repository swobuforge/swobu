package responses

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestCanonicalResponsesReplayRetainsOnlyAdmittedBehavioralState(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"type":"message","id":"msg_1","status":"completed","phase":"final_answer","role":"assistant","content":"done","unknown_known_field":true},{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"brief"}],"encrypted_content":"cipher"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	document, err := EncodeCarrierWithDecisions(
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

func TestResponsesUnknownRequestItemKindIsRejected(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"type":"future_input","value":1}]}`)
	if _, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
	); err == nil {
		t.Fatal("unknown Responses request item kind was accepted")
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

			document, err := EncodeCarrierWithDecisions(
				EncodeInput{Request: decoded.Request.Request},
				delivery.BufferedDelivery(), nil, "", EncodeOptions{},
			)
			if err != nil {
				t.Fatalf("EncodeCarrierWithDecisions returned err=%v", err)
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
