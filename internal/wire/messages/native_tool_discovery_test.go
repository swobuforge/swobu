package messages

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestMessagesNativeDiscoveryEncodesRegexAndDeferredTools(t *testing.T) {
	request, function := nativeDiscoveryRequest(t)
	doc, err := EncodeCarrier(request, delivery.BufferedDelivery())
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Tools []ProviderRequestTool `json:"tools"`
	}
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Tools) != 2 {
		t.Fatalf("tools=%d want 2", len(payload.Tools))
	}
	if payload.Tools[0].Type != toolSearchRegexType || payload.Tools[0].Name != toolSearchRegexName || payload.Tools[0].DeferLoading {
		t.Fatalf("discovery tool=%+v", payload.Tools[0])
	}
	if payload.Tools[1].Name == "" || !payload.Tools[1].DeferLoading {
		t.Fatalf("deferred function=%+v", payload.Tools[1])
	}
	if function.Key().Name() == "" {
		t.Fatal("function key is empty")
	}
}

func TestMessagesClientDecodePreservesNativeDiscoveryHistory(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"tools":[
			{"type":"tool_search_tool_regex_20251119","name":"tool_search_tool_regex"},
			{"name":"weather","input_schema":{"type":"object"},"defer_loading":true}
		],
		"messages":[
			{"role":"assistant","content":[{"type":"server_tool_use","id":"search_1","name":"tool_search_tool_regex","input":{"pattern":"weather"}}]},
			{"role":"user","content":[{"type":"tool_search_tool_result","tool_use_id":"search_1","content":{"type":"tool_search_tool_search_result","tool_references":[{"type":"tool_reference","tool_name":"weather"}]}}]}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(protocolkind.Messages, "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 3 {
		t.Fatalf("items=%d want 3", len(items))
	}
	declarations, _ := items[0].ToolDeclarations()
	if !declarations.Visibility().Deferred(declarations.Tools().Declarations()[1].Key()) {
		t.Fatal("deferred tool visibility was not preserved")
	}
	call, ok := items[1].ToolCall()
	if !ok || call.Tool().Kind() != canonical.ToolKindDiscovery {
		t.Fatalf("call=%+v ok=%t", call, ok)
	}
	result, ok := items[2].ToolDiscoveryResult()
	if !ok || len(result.Tools().Declarations()) != 1 {
		t.Fatalf("result=%+v ok=%t", result, ok)
	}
}

func TestMessagesClientDecodeResolvesDeferredWebSearchReference(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"tools":[
			{"type":"tool_search_tool_regex_20251119","name":"tool_search_tool_regex"},
			{"type":"web_search_20260209","name":"web_search","defer_loading":true}
		],
		"messages":[
			{"role":"assistant","content":[{"type":"server_tool_use","id":"search_1","name":"tool_search_tool_regex","input":{"pattern":"web"}}]},
			{"role":"user","content":[{"type":"tool_search_tool_result","tool_use_id":"search_1","content":{"type":"tool_search_tool_search_result","tool_references":[{"type":"tool_reference","tool_name":"web_search"}]}}]}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(protocolkind.Messages, "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	result, ok := items[len(items)-1].ToolDiscoveryResult()
	if !ok || len(result.Tools().Declarations()) != 1 || result.Tools().Declarations()[0].Kind() != canonical.ToolKindWebSearch {
		t.Fatalf("result=%+v ok=%t", result, ok)
	}
}

func TestMessagesBufferedDiscoveryResolvesDeferredWebSearchReference(t *testing.T) {
	discoverySchema, _ := canonical.ParseJSONObject([]byte(`{"type":"object","properties":{"pattern":{"type":"string"}}}`))
	discovery, _ := canonical.NewToolDiscoveryToolWithQuery("find tools", canonical.NewToolSchemaObject(discoverySchema), canonical.DiscoveryExecutorProvider, canonical.ToolDiscoveryQueryRegex)
	webSearch := canonical.NewWebSearchDeclaration()
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, webSearch})
	visibility, _ := canonical.NewToolVisibilityRefinements(tools, []canonical.ToolKey{webSearch.Key()})
	declarations, _ := canonical.NewToolDeclarationsItemWithVisibility(tools, canonical.ContextScopeRequest, visibility)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{declarations}})
	names := testAttemptToolNames(request)
	raw := []byte(`{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"search_1","name":"tool_search_tool_regex","input":{"pattern":"web"}},{"type":"tool_search_tool_result","tool_use_id":"search_1","content":{"type":"tool_search_tool_search_result","tool_references":[{"type":"tool_reference","tool_name":"web_search"}]}}]}`)
	reader, err := decodeResponseBuffered(context.Background(), request, names, raw, "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := readNativeDiscoveryResponse(t, reader)
	result, ok := response.Items()[1].ToolDiscoveryResult()
	if !ok || len(result.Tools().Declarations()) != 1 || result.Tools().Declarations()[0].Kind() != canonical.ToolKindWebSearch {
		t.Fatalf("result=%+v ok=%t", result, ok)
	}
}

func TestMessagesBufferedDiscoveryFailureIsTyped(t *testing.T) {
	request, _ := nativeDiscoveryRequest(t)
	names := testAttemptToolNames(request)
	raw := []byte(`{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"search_1","name":"tool_search_tool_regex","input":{"pattern":"weather"}},{"type":"tool_search_tool_result","tool_use_id":"search_1","content":{"type":"tool_search_tool_result_error","error_code":"unavailable","error_message":"search offline"}}]}`)
	reader, err := decodeResponseBuffered(context.Background(), request, names, raw, "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := readNativeDiscoveryResponse(t, reader)
	result, ok := response.Items()[1].ToolDiscoveryResult()
	if !ok {
		t.Fatal("buffered result is not discovery")
	}
	failure, ok := result.Failure()
	code, _ := failure.Code().Get()
	if !ok || code != "unavailable" || failure.Message() != "search offline" {
		t.Fatalf("failure=(%+v,%t)", failure, ok)
	}
}

func TestMessagesClientDiscoveryFailureUsesOrdinaryToolResult(t *testing.T) {
	discoverySchema, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	discovery, err := canonical.NewToolDiscoveryTool("find tools", canonical.NewToolSchemaObject(discoverySchema), canonical.DiscoveryExecutorClient)
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery})
	declarations, _ := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	callID, _ := canonical.NewToolCallID("search_1")
	input, _ := canonical.ParseJSONObject([]byte(`{"query":"weather"}`))
	call, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(input), canonical.DiscoveryExecutorClient)
	failure, _ := canonical.NewToolDiscoveryFailureItem(callID, canonical.DiscoveryExecutorClient, canonical.Specify("unavailable"), "search offline")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{declarations, call, failure}})
	doc, err := EncodeCarrier(request, delivery.BufferedDelivery())
	if err != nil {
		t.Fatal(err)
	}
	raw := string(doc.RawBytes())
	if !strings.Contains(raw, `"type":"tool_result"`) || !strings.Contains(raw, `"is_error":true`) {
		t.Fatalf("client discovery failure wire=%s", raw)
	}
	if strings.Contains(raw, "tool_search_tool_result_error") || strings.Contains(raw, `"type":"tool_search_tool_result"`) {
		t.Fatalf("client discovery failure used server grammar: %s", raw)
	}
}

func TestMessagesStreamingDiscoverySuccessIsTyped(t *testing.T) {
	request, _ := nativeDiscoveryRequest(t)
	names := testAttemptToolNames(request)
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\",\"usage\":{\"input_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"search_1\",\"name\":\"tool_search_tool_regex\",\"input\":{\"pattern\":\"weather\"}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_search_tool_result\",\"tool_use_id\":\"search_1\",\"content\":{\"type\":\"tool_search_tool_search_result\",\"tool_references\":[{\"type\":\"tool_reference\",\"tool_name\":\"weather\"}]}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	reader := decodeResponseStream(request, names, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	response := readNativeDiscoveryResponse(t, reader)
	items := response.Items()
	if len(items) != 2 || items[0].Kind() != canonical.ItemKindToolCall || items[1].Kind() != canonical.ItemKindToolDiscoveryResult {
		t.Fatalf("items=%v", items)
	}
}

func nativeDiscoveryRequest(t *testing.T) (canonical.CanonicalRequest, canonical.ToolDeclaration) {
	t.Helper()
	discoverySchema, _ := canonical.ParseJSONObject([]byte(`{"type":"object","properties":{"pattern":{"type":"string"}}}`))
	discovery, err := canonical.NewToolDiscoveryToolWithQuery("find tools", canonical.NewToolSchemaObject(discoverySchema), canonical.DiscoveryExecutorProvider, canonical.ToolDiscoveryQueryRegex)
	if err != nil {
		t.Fatal(err)
	}
	functionKey, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "weather")
	functionSchema, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	function, err := canonical.NewFunctionTool(functionKey, "weather", canonical.NewToolSchemaObject(functionSchema), canonical.Unspecified[bool]())
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, function})
	visibility, _ := canonical.NewToolVisibilityRefinements(tools, []canonical.ToolKey{function.Key()})
	declarations, _ := canonical.NewToolDeclarationsItemWithVisibility(tools, canonical.ContextScopeRequest, visibility)
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("find weather")})
	return canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{declarations, message}}), function
}

func readNativeDiscoveryResponse(t *testing.T, reader canonical.ResponseStream) *canonical.CanonicalResponse {
	t.Helper()
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	return response
}
