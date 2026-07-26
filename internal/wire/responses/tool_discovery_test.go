package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestToolDiscoveryLifecycleLoadsDeclarationsInOrder(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"input":[
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"tool_search","execution":"client","description":"find tools","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}
			]},
			{"type":"tool_search_call","call_id":"search_1","execution":"client","arguments":{"query":"files"}},
			{"type":"tool_search_output","call_id":"search_1","status":"completed","execution":"client","tools":[
				{"type":"namespace","name":"workspace","tools":[
					{"type":"function","name":"read_file","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}
				]}
			]},
			{"type":"function_call","call_id":"call_1","name":"read_file","arguments":{"path":"README.md"}}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 4 ||
		items[0].Kind() != canonical.ItemKindToolDeclarations ||
		items[1].Kind() != canonical.ItemKindToolCall ||
		items[2].Kind() != canonical.ItemKindToolDiscoveryResult ||
		items[3].Kind() != canonical.ItemKindToolCall {
		t.Fatalf("discovery lifecycle = %#v", items)
	}
	before, err := canonical.ToolEnvironmentAt(items, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Declarations()) != 1 || before.Declarations()[0].Kind() != canonical.ToolKindDiscovery {
		t.Fatalf("environment before output = %#v", before.Declarations())
	}
	after, err := canonical.ToolEnvironmentAt(items, 3)
	if err != nil {
		t.Fatal(err)
	}
	call, _ := items[3].ToolCall()
	if _, ok := after.Lookup(call.Tool()); !ok {
		t.Fatalf("loaded environment cannot resolve %q", call.Tool())
	}

	document, err := EncodeCarrier(decoded.Request.Request, delivery.BufferedDelivery())
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`"type":"tool_search"`, `"type":"tool_search_call"`, `"type":"tool_search_output"`, `"type":"namespace"`} {
		if !strings.Contains(string(document.RawBytes()), marker) {
			t.Fatalf("encoded discovery lifecycle lost %s: %s", marker, document.RawBytes())
		}
	}
}

func TestProviderToolDiscoveryOutputDecodesSemantically(t *testing.T) {
	schema, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	discovery, err := canonical.NewToolDiscoveryTool("find tools", canonical.NewToolSchemaObject(schema), canonical.DiscoveryExecutorProvider)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery})
	declarations, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarations}})
	var wireItems []json.RawMessage
	if err := json.Unmarshal([]byte(`[
		{"type":"tool_search_call","call_id":"search_1","execution":"server","arguments":{"query":"files"}},
		{"type":"tool_search_output","call_id":"search_1","status":"completed","execution":"server","tools":[
			{"type":"function","name":"read_file","parameters":{"type":"object"}}
		]}
	]`), &wireItems); err != nil {
		t.Fatal(err)
	}
	items, err := decodeCompletedResponsesItemSet(t.Context(), request, wireItems, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Kind() != canonical.ItemKindToolCall || items[1].Kind() != canonical.ItemKindToolDiscoveryResult {
		t.Fatalf("provider discovery output = %#v", items)
	}
	call, _ := items[0].ToolCall()
	callExecutor, ok := call.DiscoveryExecutor()
	result, _ := items[1].ToolDiscoveryResult()
	if !ok || callExecutor != canonical.DiscoveryExecutorProvider ||
		result.Executor() != canonical.DiscoveryExecutorProvider ||
		items[1].Owner() != canonical.TurnOwnerAssistant {
		t.Fatalf("provider discovery ownership was not retained: call=%v result=%v", callExecutor, result.Executor())
	}
	response := canonicaltest.Response(t, "resp_discovery", "m", items, "completed")
	document, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(request, response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.Document.RawBytes()), `"execution":"server"`) {
		t.Fatalf("provider discovery re-encoded with wrong ownership: %s", document.Document.RawBytes())
	}
}
