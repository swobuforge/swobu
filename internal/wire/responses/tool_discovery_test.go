package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestHistoricalDiscoveryWithoutDeclarationProvenanceDoesNotRunTransformer(t *testing.T) {
	object, _ := canonical.ParseJSONObject([]byte(`{"query":"files"}`))
	callID, _ := canonical.NewToolCallID("search_without_declaration")
	item, err := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(object), canonical.DiscoveryExecutorClient)
	if err != nil {
		t.Fatal(err)
	}
	call, _ := item.ToolCall()
	invoked := false
	lowering := DefaultToolLowering().Overlay(ToolLowering{Discovery: func(_ ToolLoweringContext, _ canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		invoked = true
		return ToolProjection{}, nil, nil
	}})
	projection := responsesToolProjection{lowered: wire.LoweredToolSet{}, lowering: lowering}
	occurrence, err := projection.historicalProjection(call, nil)
	if err != nil {
		t.Fatal(err)
	}
	if invoked {
		t.Fatal("historical Discovery fabricated a declaration and ran the selected transformer")
	}
	if occurrence.ProjectCall != nil || occurrence.ProjectResult != nil {
		t.Fatalf("history without declaration provenance recovered executable projection: %#v", occurrence)
	}
}

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

	names, _, err := provider.BuildAttemptToolNames(decoded.Request.Request)
	if err != nil {
		t.Fatal(err)
	}
	wireName, _ := names.WireName(call.Tool())
	document, err := EncodeCarrierWithChanges(EncodeInput{Request: decoded.Request.Request, ToolNames: names}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`"type":"tool_search"`, `"type":"tool_search_call"`, `"type":"tool_search_output"`, `"name":"` + wireName + `"`} {
		if !strings.Contains(string(document.RawBytes()), marker) {
			t.Fatalf("encoded discovery lifecycle lost %s: %s", marker, document.RawBytes())
		}
	}
	if strings.Contains(string(document.RawBytes()), `"type":"namespace"`) {
		t.Fatalf("eager lowering retained discovery namespace container: %s", document.RawBytes())
	}
}

func TestToolDiscoveryRequestRejectsAllErasedResultDeclarations(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"input":[
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"tool_search","execution":"server","description":"find tools","parameters":{"type":"object"}}
			]},
			{"type":"tool_search_call","call_id":"search_1","execution":"server","arguments":{}},
			{"type":"tool_search_output","call_id":"search_1","status":"completed","execution":"server","tools":[
				{"type":"future_tool"}
			]}
		]
	}`)
	if _, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	); err == nil {
		t.Fatal("all-erased request discovery result fabricated an empty result")
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
	items, err := decodeCompletedResponsesItemSet(t.Context(), request, nil, wireItems, "", "", nil)
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
	response := canonicaltest.Response(t, "resp_discovery", "m", items, canonical.Completed("completed"))
	document, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(request, response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.Document.RawBytes()), `"execution":"server"`) {
		t.Fatalf("provider discovery re-encoded with wrong ownership: %s", document.Document.RawBytes())
	}
}
