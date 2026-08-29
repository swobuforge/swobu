package chatcompletions

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestChatCompletionsEagerlyMaterializesProviderOwnedDiscovery(t *testing.T) {
	discovery, err := canonical.NewToolDiscoveryTool(
		"find a tool",
		canonicaltest.Schema(t, `{"type":"object"}`),
		canonical.DiscoveryExecutorProvider,
	)
	if err != nil {
		t.Fatal(err)
	}
	loaded := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "weather"), "weather", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, discovery, loaded),
			canonicaltest.Message(t, canonical.MessageRoleUser, "find an appropriate tool"),
		},
	})
	var changes []compat.Change
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, err := CompileProviderRequestDocument(request, names, delivery.BufferedDelivery(), &changes, "exchange", CompileOptions{Lowering: DefaultLowering()})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Tools) != 1 || document.Tools[0].Function == nil || document.Tools[0].Function.Name != "weather" {
		t.Fatalf("tools=%#v, want eager weather function only", document.Tools)
	}
	if len(changes) != 1 || changes[0].Capability != canonical.RequestToolsVisibility || changes[0].Kind != compat.Approximation {
		t.Fatalf("changes=%#v, want one visibility approximation", changes)
	}
}

func TestChatCompletionsOmitsSettledProviderOwnedDiscoveryHistoryAtomically(t *testing.T) {
	discovery, err := canonical.NewToolDiscoveryTool(
		"find a tool",
		canonicaltest.Schema(t, `{"type":"object"}`),
		canonical.DiscoveryExecutorProvider,
	)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := canonical.NewToolCallID("discovery_1")
	input := canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"query":"weather"}`))
	call, err := canonical.NewToolDiscoveryCallItem(callID, input, canonical.DiscoveryExecutorProvider)
	if err != nil {
		t.Fatal(err)
	}
	loaded := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "weather"), "weather", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	result, err := canonical.NewToolDiscoveryResultItem(callID, canonicaltest.ToolSet(t, loaded), canonical.DiscoveryExecutorProvider)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, discovery),
			canonicaltest.Message(t, canonical.MessageRoleUser, "find an appropriate tool"),
			call,
			result,
			canonicaltest.Message(t, canonical.MessageRoleUser, "continue"),
		},
	})
	var changes []compat.Change
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, err := CompileProviderRequestDocument(request, names, delivery.BufferedDelivery(), &changes, "exchange", CompileOptions{Lowering: DefaultLowering()})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Messages) != 2 {
		t.Fatalf("messages=%#v, want only user messages", document.Messages)
	}
	if len(changes) != 2 || changes[0].Capability != canonical.RequestItemsKind || changes[0].Kind != compat.Omission ||
		changes[1].Capability != canonical.RequestToolsVisibility || changes[1].Kind != compat.Approximation {
		t.Fatalf("changes=%#v, want atomic effect omission plus visibility approximation", changes)
	}
}

func TestChatCompletionsRetainsDiscoveredDeclarationForLaterHistoricalCall(t *testing.T) {
	discovery, err := canonical.NewToolDiscoveryTool(
		"find a tool",
		canonicaltest.Schema(t, `{"type":"object"}`),
		canonical.DiscoveryExecutorProvider,
	)
	if err != nil {
		t.Fatal(err)
	}
	loaded := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "read_file"), "read", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	discoveryCallID, _ := canonical.NewToolCallID("search_1")
	discoveryCall, _ := canonical.NewToolDiscoveryCallItem(discoveryCallID, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"query":"files"}`)), canonical.DiscoveryExecutorProvider)
	discoveryResult, _ := canonical.NewToolDiscoveryResultItem(discoveryCallID, canonicaltest.ToolSet(t, loaded), canonical.DiscoveryExecutorProvider)
	toolCallID, _ := canonical.NewToolCallID("call_read")
	toolCall, _ := canonical.NewToolCallItem(toolCallID, loaded.Key(), canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"path":"README.md"}`)))
	toolResult, _ := canonical.NewToolResultItem(toolCallID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("contents")}, false)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, discovery),
			discoveryCall,
			discoveryResult,
			toolCall,
			toolResult,
			canonicaltest.Message(t, canonical.MessageRoleUser, "summarize"),
		},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, err := CompileProviderRequestDocument(request, names, delivery.BufferedDelivery(), nil, "exchange", CompileOptions{Lowering: DefaultLowering()})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Tools) != 1 || document.Tools[0].Function == nil || document.Tools[0].Function.Name != "read_file" {
		t.Fatalf("tools=%#v, want discovered read_file declaration", document.Tools)
	}
	if len(document.Messages) != 3 || len(document.Messages[0].ToolCalls) != 1 || document.Messages[0].ToolCalls[0].Function == nil || document.Messages[0].ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("messages=%#v, want discovered historical call/result plus user continuation", document.Messages)
	}
}
