package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/continuity"
)

func TestResponsesAllErasedToolResultDoesNotClosePendingCall(t *testing.T) {
	raw := []byte(`{"input":[
		{"type":"function_call","call_id":"call_1","name":"lookup","arguments":{}},
		{"type":"function_call_output","call_id":"call_1","output":[
			{"type":"future_result"}
		]}
	]}`)
	if _, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{})); err == nil {
		t.Fatal("all-erased Responses tool result closed its pending call")
	}
}

func TestResponsesAllUnknownRequestItemsRejectAfterSessionMaterialization(t *testing.T) {
	raw := []byte(`{"input":[{"type":"future_item","value":"ignored"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatalf("wire decoder rejected additive unknown item too early: %v", err)
	}
	if _, err := continuity.Begin(decoded.Request.Request); err == nil {
		t.Fatal("session accepted all-erased materialized request")
	}
}

func TestResponsesRequiredToolPolicyRejectsAllErasedToolsAfterSessionMaterialization(t *testing.T) {
	raw := []byte(`{
		"tools":[{"type":"future_tool","name":"ignored"}],
		"tool_choice":"required",
		"input":[{"type":"message","role":"user","content":"run"}]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatalf("wire decoder rejected residual environment too early: %v", err)
	}
	if _, err := continuity.Begin(decoded.Request.Request); err == nil {
		t.Fatal("required policy survived after every materialized tool was erased")
	}
}

func TestResponsesUnknownCorrelationMetadataDoesNotClaimKnownCallID(t *testing.T) {
	raw := []byte(`{"input":[
		{"type":"function_call","call_id":"call_1","name":"search","arguments":{}},
		{"type":"future_call_metadata","call_id":"call_1"},
		{"type":"function_call_output","call_id":"call_1","output":"done"}
	]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 2 {
		t.Fatalf("canonical items = %#v, want known call and result", items)
	}
	if _, ok := items[0].ToolCall(); !ok {
		t.Fatalf("first item = %#v, want tool call", items[0])
	}
	if _, ok := items[1].ToolResult(); !ok {
		t.Fatalf("second item = %#v, want tool result", items[1])
	}
}

func TestResponsesRequestPreservesWebSearchCallWhenStatusIsUnknown(t *testing.T) {
	raw := []byte(`{"model":"m","input":[
		{"type":"web_search_call","id":"ws_1","status":"future_status","action":{"type":"search","queries":["q"]}},
		{"type":"message","role":"user","content":"kept"}
	]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 2 {
		t.Fatalf("canonical request items = %#v, want web-search call and message", items)
	}
	call, ok := items[0].ToolCall()
	if !ok || call.CallID().String() != "ws_1" {
		t.Fatalf("first item = %#v, want preserved ws_1 call", items[0])
	}
	search, ok := call.Input().WebSearch()
	if !ok || search.Action != canonical.WebSearchActionSearch || len(search.Queries) != 1 || search.Queries[0] != "q" {
		t.Fatalf("web-search input = %#v, want search query q", call.Input())
	}
	if _, ok := items[1].Message(); !ok {
		t.Fatalf("second item = %#v, want surviving message", items[1])
	}
	for _, item := range items {
		if _, ok := item.ToolResult(); ok {
			t.Fatalf("unknown lifecycle status manufactured result: %#v", item)
		}
	}
	if len(decoded.Changes) != 1 {
		t.Fatalf("changes = %#v, want occurrence-local status omission", decoded.Changes)
	}
	item, occurrenceOK := decoded.Changes[0].Occurrence.RequestItem()
	if decoded.Changes[0].Kind != compat.Omission || !occurrenceOK || item != 0 {
		t.Fatalf("changes = %#v, want occurrence-local status Drop", decoded.Changes)
	}
}
