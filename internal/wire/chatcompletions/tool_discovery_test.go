package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestChatCompletionsLowersClientDiscoveryThroughFunctionLifecycle(t *testing.T) {
	discovery, err := canonical.NewToolDiscoveryTool("find tools", canonicaltest.Schema(t, `{"type":"object","properties":{"query":{"type":"string"}}}`), canonical.DiscoveryExecutorClient)
	if err != nil {
		t.Fatal(err)
	}
	loaded := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "read_file"), "read", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	declarations := canonicaltest.ToolDeclarations(t, discovery, loaded)
	callID, _ := canonical.NewToolCallID("search_1")
	input, _ := canonical.ParseJSONObject([]byte(`{"query":"files"}`))
	call, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(input), canonical.DiscoveryExecutorClient)
	loadedSet, _ := canonical.NewToolSet([]canonical.ToolDeclaration{loaded})
	result, _ := canonical.NewToolDiscoveryResultItem(callID, loadedSet, canonical.DiscoveryExecutorClient)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{declarations, call, result}})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, err := CompileProviderRequestDocument(request, names, delivery.BufferedDelivery(), nil, "exchange", CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Tools) != 2 || document.Tools[0].Type != "function" || document.Tools[0].Function == nil {
		t.Fatalf("tools=%#v, want lowered discovery function", document.Tools)
	}
	wantName, err := names.WireName(discovery.Key())
	if err != nil {
		t.Fatal(err)
	}
	if document.Tools[0].Function.Name != wantName {
		t.Fatalf("discovery name=%q, want attempt mapping %q", document.Tools[0].Function.Name, wantName)
	}
	if len(document.Messages) != 2 || len(document.Messages[0].ToolCalls) != 1 || document.Messages[0].ToolCalls[0].Function == nil {
		t.Fatalf("messages=%#v, want ordinary function call", document.Messages)
	}
	var resultPayload map[string][]string
	if err := json.Unmarshal([]byte(document.Messages[1].Content.(string)), &resultPayload); err != nil {
		t.Fatal(err)
	}
	if got := resultPayload["loaded_tools"]; len(got) != 1 || got[0] != loaded.Key().String() {
		t.Fatalf("loaded_tools=%v", got)
	}
}

func TestChatCompletionsGenericDiscoveryReverseMappingUsesAttemptProvenance(t *testing.T) {
	discovery, err := canonical.NewToolDiscoveryTool("find tools", canonicaltest.Schema(t, `{"type":"object"}`), canonical.DiscoveryExecutorClient)
	if err != nil {
		t.Fatal(err)
	}
	declarations := canonicaltest.ToolDeclarations(t, discovery)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{declarations}})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	name, err := names.WireName(discovery.Key())
	if err != nil {
		t.Fatal(err)
	}
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		t.Fatal(err)
	}
	key, err := wire.DecodeCallableKey(names, environment, name)
	if err != nil || key != discovery.Key() {
		t.Fatalf("reverse provenance = %q, %v; want %q", key, err, discovery.Key())
	}
}
