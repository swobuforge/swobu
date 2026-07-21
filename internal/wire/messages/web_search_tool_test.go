package messages

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeMessagesWebSearchDropsDynamicCallerPreference(t *testing.T) {
	base := ProviderRequestTool{Type: "web_search_20260209", Name: "web_search"}
	base.AllowedCallers = []string{"code_execution"}
	declaration, err := decodeMessagesWebSearchTool(base, 0, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if declaration.Kind() != canonical.ToolKindWebSearch {
		t.Fatal("decoded declaration is not web search")
	}
}

func TestDecodeMessagesWebSearchDropsAllWirePreferences(t *testing.T) {
	raw := []byte(`{"model":"model","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"web_search_20260318","name":"web_search","max_uses":3,"allowed_domains":["example.com"],"blocked_domains":["blocked.example"],"user_location":{"type":"approximate","country":"GB"},"allowed_callers":["code_execution"],"response_inclusion":"full"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if tools := decoded.Request.Request.Tools(); len(tools) != 1 || tools[0].Kind() != canonical.ToolKindWebSearch {
		t.Fatalf("tools = %#v", tools)
	}
	want := map[compat.Subject]bool{}
	for _, field := range []string{"max_uses", "allowed_domains", "blocked_domains", "user_location", "allowed_callers", "response_inclusion"} {
		want[compat.Subject("wire:/tools/0/"+field)] = true
	}
	if len(decoded.Decisions) != len(want) {
		t.Fatalf("decisions = %#v", decoded.Decisions)
	}
	for _, decision := range decoded.Decisions {
		if decision.Feature != compat.RequestTools || decision.Outcome != compat.Drop || !want[decision.Subject] {
			t.Fatalf("decision = %#v", decision)
		}
	}
}

func TestEncodeMessagesWebSearchUsesNeutralMarker(t *testing.T) {
	tools, err := encodeMessagesTools([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()}, nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(tools[0])
	if string(raw) != `{"type":"web_search","name":"web_search"}` {
		t.Fatalf("tool = %s", raw)
	}
}

func TestDecodeResponseBufferedPreservesWebSearchLifecycle(t *testing.T) {
	raw := []byte(`{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"x"}},{"type":"web_search_tool_result","tool_use_id":"s","content":[{"type":"web_search_result","url":"https://example.com/x","title":"Example"}]},{"type":"text","text":"answer","citations":[{"type":"web_search_result_location","url":"https://example.com/x","title":"Example"}]}]}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 3 || items[0].Kind() != canonical.ItemKindToolCall || items[1].Kind() != canonical.ItemKindToolResult || items[2].Kind() != canonical.ItemKindMessage {
		t.Fatalf("items = %#v", items)
	}
	result, _ := items[1].ToolResult()
	search, ok := result.WebSearch()
	if !ok || len(search.Sources()) != 1 {
		t.Fatalf("search result = %#v, %v", search, ok)
	}
}

func TestDecodeResponseBufferedPreservesWebSearchFailure(t *testing.T) {
	raw := []byte(`{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"x"}},{"type":"web_search_tool_result","tool_use_id":"s","content":{"type":"web_search_tool_result_error","error_code":"unavailable"}}]}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	result, ok := response.Items()[1].ToolResult()
	if !ok {
		t.Fatal("web-search failure did not decode as a tool result")
	}
	search, ok := result.WebSearch()
	if failure, failed := search.Failure(); !ok || !failed || failure != "unavailable" {
		t.Fatalf("search failure = %q, %v, web search = %v", failure, failed, ok)
	}
}

func TestEncodeMessagesWebSearchFailureUsesObjectContent(t *testing.T) {
	failure, err := canonical.NewWebSearchFailureResult("unavailable")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encodeMessagesWebSearchResult(failure)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"type":"web_search_tool_result_error","error_code":"unavailable"}` {
		t.Fatalf("content = %s", raw)
	}
}

func TestDecodeResponseStreamRejectsUnknownServerToolLifecycle(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" + "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"s\",\"name\":\"code_execution\"}}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	if _, err := reader.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(context.Background()); err == nil {
		t.Fatal("unknown server tool stream lifecycle was silently dropped")
	}
}

func TestDecodeResponseStreamPreservesWebSearchLifecycle(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"s\",\"name\":\"web_search\",\"input\":{}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"query\\\":\\\"x\\\"}\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"s\",\"content\":[{\"type\":\"web_search_result\",\"url\":\"https://example.com/x\"}]}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items()) != 2 {
		t.Fatalf("items=%d want=2", len(response.Items()))
	}
}

func TestDecodeResponseStreamPreservesWebSearchFailure(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"s\",\"name\":\"web_search\",\"input\":{\"query\":\"x\"}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"s\",\"content\":{\"type\":\"web_search_tool_result_error\",\"error_code\":\"too_many_requests\"}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	result, ok := response.Items()[1].ToolResult()
	if !ok {
		t.Fatal("web-search failure did not decode as a tool result")
	}
	search, ok := result.WebSearch()
	if failure, failed := search.Failure(); !ok || !failed || failure != "too_many_requests" {
		t.Fatalf("search failure = %q, %v, web search = %v", failure, failed, ok)
	}
}
