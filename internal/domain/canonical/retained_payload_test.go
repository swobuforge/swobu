package canonical

import (
	"strings"
	"testing"
)

func TestRetainedPayloadWeightCoversVariableSubtypeBranches(t *testing.T) {
	base := retainedPayloadFixtures(t, "")
	large := retainedPayloadFixtures(t, strings.Repeat("x", 113))
	if len(base) != len(large) {
		t.Fatalf("fixture count differs: base=%d large=%d", len(base), len(large))
	}
	for index := range base {
		baseBytes, _, err := RetainedPayloadWeight(base[index].item)
		if err != nil {
			t.Fatalf("%s base: %v", base[index].name, err)
		}
		largeBytes, _, err := RetainedPayloadWeight(large[index].item)
		if err != nil {
			t.Fatalf("%s large: %v", large[index].name, err)
		}
		if largeBytes-baseBytes < 113 {
			t.Fatalf("%s variable payload delta=%d, want at least 113", base[index].name, largeBytes-baseBytes)
		}
	}
}

func TestRetainedPayloadWeightDoesNotCloneLargePayload(t *testing.T) {
	image, err := NewInlineImage(ImageMediaPNG, make([]byte, 16<<20), Unspecified[ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewMessageItem(MessageRoleUser, []MessagePart{NewImageMessagePart(image)})
	if err != nil {
		t.Fatal(err)
	}
	bytes, _, err := RetainedPayloadWeight(item)
	if err != nil || bytes < 16<<20 {
		t.Fatalf("weight bytes=%d err=%v", bytes, err)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if _, _, weightErr := RetainedPayloadWeight(item); weightErr != nil {
			panic(weightErr)
		}
	}); allocations != 0 {
		t.Fatalf("RetainedPayloadWeight allocations=%v, want 0", allocations)
	}
}

func TestRetainedRequestMetadataWeightDoesNotCloneLargeSchema(t *testing.T) {
	schema := NewRawJSONObject(`{"type":"object","description":"` + strings.Repeat("x", 16<<20) + `"}`)
	format, err := NewOutputFormat(OutputFormatParams{
		Kind: OutputFormatJSONSchema, Name: "large", Schema: schema,
		SchemaContract: SchemaContract{Profile: SchemaProfileUnprofiled},
	})
	if err != nil {
		t.Fatal(err)
	}
	controls, err := NewGenerationControls(GenerationControlsParams{StopSequences: []string{strings.Repeat("s", 4096)}})
	if err != nil {
		t.Fatal(err)
	}
	request := NewCanonicalRequest(RequestParams{Model: Specify("m"), Controls: controls, OutputFormat: Specify(format)})
	if allocations := testing.AllocsPerRun(100, func() {
		bytes, _, weightErr := RetainedRequestMetadataWeight(request)
		if weightErr != nil || bytes < 16<<20 {
			panic("request metadata weight failed")
		}
	}); allocations != 0 {
		t.Fatalf("RetainedRequestMetadataWeight allocations=%v, want 0", allocations)
	}
}

type retainedPayloadFixture struct {
	name string
	item CanonicalItem
}

func retainedPayloadFixtures(t *testing.T, variable string) []retainedPayloadFixture {
	t.Helper()
	callID, _ := NewToolCallID("call")
	webInput, _ := NewWebSearchToolInput(WebSearchCall{Action: WebSearchActionSearch, Queries: []string{"query"}})
	refinement, _ := NewResponsesWebSearchRefinement(ResponsesItemID("item-" + variable))
	webCall, err := NewToolCallItemWithResponsesWebSearch(callID, WebSearchToolKey(), webInput, &refinement)
	if err != nil {
		t.Fatal(err)
	}

	always, _ := NewMCPToolFilter(Specify([]string{"always-" + variable}), Unspecified[bool]())
	never, _ := NewMCPToolFilter(Specify([]string{"never-" + variable}), Unspecified[bool]())
	approval, _ := NewMCPApprovalFilter(&always, &never)
	source, err := NewMCPURLSource("https://mcp.example/"+variable, Specify([]string{"allowed-" + variable}), approval, MCPLoadingDeferred, Specify([]string{"caller-" + variable}))
	if err != nil {
		t.Fatal(err)
	}
	mcpKey, _ := NewRequestToolKey(ToolKindMCP, "remote")
	functionKey, _ := NewRequestToolKey(ToolKindFunction, "lookup")
	schema, _ := ParseJSONObject([]byte(`{"type":"object"}`))
	function, _ := NewFunctionTool(functionKey, variable, NewToolSchemaObject(schema), SchemaContract{Profile: SchemaProfileUnprofiled})
	mcp, err := NewMCPToolSource(mcpKey, variable, source, []ToolDeclaration{function})
	if err != nil {
		t.Fatal(err)
	}
	set, _ := NewToolSet([]ToolDeclaration{mcp})
	declarations, _ := NewToolDeclarationsItem(set, ContextScopeHistory)

	opaque, _ := NewProviderChatOpaqueThinking(ProviderChatReplayScope("scope-"+variable), []byte("raw"))
	opaque, _ = opaque.withTargetOrigin("target-"+variable, 1)
	reasoning, _ := NewReasoningItem(nil, opaque)

	return []retainedPayloadFixture{
		{name: "responses web-search item id", item: webCall},
		{name: "MCP selection and approval filters", item: declarations},
		{name: "opaque scope and origin", item: reasoning},
	}
}
