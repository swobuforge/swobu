package canonical

import "testing"

func newCanonicalTestMCPURL(endpoint string, allowed Specified[[]string]) (MCPSource, error) {
	return NewMCPURLSource(
		endpoint, allowed, NewMCPApprovalNever(), MCPLoadingEager,
		Unspecified[[]string](),
	)
}

func TestMCPToolSourceOwnsAuthorityCatalogAndDerivedLookup(t *testing.T) {
	sourceKey, _ := NewToolKey("mcp", ToolKindMCP, "docs")
	source, err := newCanonicalTestMCPURL("https://mcp.example.test/rpc", Specify([]string{"search"}))
	if err != nil {
		t.Fatal(err)
	}
	toolKey, _ := NewToolKey("mcp/docs", ToolKindFunction, "search")
	schemaObject, _ := ParseJSONObject([]byte(`{"type":"object"}`))
	tool, err := NewFunctionTool(toolKey, "Search", NewToolSchemaObject(schemaObject), Unspecified[bool]())
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := NewMCPToolSource(sourceKey, "Docs", source, []ToolDeclaration{tool})
	if err != nil {
		t.Fatal(err)
	}
	set, _ := NewToolSet([]ToolDeclaration{namespace})
	item, _ := NewToolDeclarationsItem(set, ContextScopeHistory)
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{item}})

	if len(request.Items()) != 1 {
		t.Fatalf("remote MCP aggregate split across items: %#v", request.Items())
	}
	environment, err := EffectiveTools(request)
	if err != nil {
		t.Fatal(err)
	}
	declaration, ok := environment.Lookup(toolKey)
	if !ok || declaration.Key() != toolKey {
		t.Fatalf("derived namespace lookup = %#v, %v", declaration, ok)
	}
}

func TestRequestScopedRemoteMCPSourceExpiresAtomically(t *testing.T) {
	key, _ := NewToolKey("mcp", ToolKindMCP, "docs")
	source, _ := newCanonicalTestMCPURL("https://mcp.example.test/rpc", Unspecified[[]string]())
	namespace, _ := NewMCPToolSource(key, "", source, nil)
	set, _ := NewToolSet([]ToolDeclaration{namespace})
	item, _ := NewToolDeclarationsItem(set, ContextScopeRequest)
	if retained := RetainedHistory([]CanonicalItem{item}); len(retained) != 0 {
		t.Fatalf("request-scoped remote MCP source survived: %#v", retained)
	}
}

func TestRemoteMCPSourceEquivalenceIncludesSelection(t *testing.T) {
	left, _ := newCanonicalTestMCPURL("https://mcp.example.test/rpc", Specify([]string{"search"}))
	same, _ := newCanonicalTestMCPURL("https://mcp.example.test/rpc", Specify([]string{"search"}))
	different, _ := newCanonicalTestMCPURL("https://mcp.example.test/rpc", Specify([]string{"fetch"}))
	if !left.Equivalent(same) {
		t.Fatal("equivalent MCP sources differ")
	}
	if left.Equivalent(different) {
		t.Fatal("different MCP selection was treated as equivalent")
	}
}

func TestMCPSourceEquivalenceIncludesKnownAuthorityRefinements(t *testing.T) {
	always, _ := NewMCPToolFilter(Specify([]string{"write"}), Unspecified[bool]())
	never, _ := NewMCPToolFilter(Unspecified[[]string](), Specify(true))
	approval, _ := NewMCPApprovalFilter(&always, &never)
	source, err := NewMCPConnectorSource(
		"connector_gmail", Specify([]string{"search"}), approval,
		MCPLoadingDeferred, Specify([]string{"direct"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !source.Equivalent(source.Clone()) {
		t.Fatal("MCP source clone lost a known refinement")
	}
	if source.Kind() != MCPSourceConnectorID ||
		source.Approval().Kind() != MCPApprovalFilter ||
		source.Loading() != MCPLoadingDeferred {
		t.Fatalf("source = %#v", source)
	}
	callers, callersSet := source.AllowedCallers().Get()
	if !callersSet || len(callers) != 1 || callers[0] != "direct" {
		t.Fatalf("allowed callers = %#v specified=%t", callers, callersSet)
	}
	if _, ok := source.Approval().AlwaysFilter(); !ok {
		t.Fatal("always approval filter was lost")
	}
	if _, ok := source.Approval().NeverFilter(); !ok {
		t.Fatal("never approval filter was lost")
	}

	tunnel, _ := NewMCPTunnelSource(
		"tunnel_1", Unspecified[[]string](), NewMCPApprovalNever(),
		MCPLoadingEager, Unspecified[[]string](),
	)
	if source.Equivalent(tunnel) {
		t.Fatal("distinct MCP source authority was treated as equivalent")
	}
}

func TestRemoteMCPSourceCannotBeNested(t *testing.T) {
	childKey, _ := NewToolKey("mcp", ToolKindMCP, "docs")
	source, _ := newCanonicalTestMCPURL("https://mcp.example.test/rpc", Unspecified[[]string]())
	child, err := NewMCPToolSource(childKey, "", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentKey, _ := NewRequestToolKey(ToolKindNamespace, "parent")
	if _, err := NewToolNamespace(parentKey, "", []ToolDeclaration{child}); err == nil {
		t.Fatal("nested remote MCP source entered canonical state")
	}
}
