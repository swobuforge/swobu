package canonical

import "testing"

func TestMCPNamespaceOwnsSourceCatalogScopeAndDerivedLookup(t *testing.T) {
	sourceKey, _ := NewToolKey("mcp", ToolKindNamespace, "docs")
	source, err := NewMCPSource("https://mcp.example.test/rpc", Specify([]string{"search"}))
	if err != nil {
		t.Fatal(err)
	}
	toolKey, _ := NewToolKey("mcp/docs", ToolKindFunction, "search")
	schemaObject, _ := ParseJSONObject([]byte(`{"type":"object"}`))
	tool, err := NewFunctionTool(toolKey, "Search", NewToolSchemaObject(schemaObject), Unspecified[bool]())
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := NewMCPToolNamespace(sourceKey, "Docs", source, []ToolDeclaration{tool})
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

func TestRequestScopedRemoteMCPNamespaceExpiresAtomically(t *testing.T) {
	key, _ := NewToolKey("mcp", ToolKindNamespace, "docs")
	source, _ := NewMCPSource("https://mcp.example.test/rpc", Unspecified[[]string]())
	namespace, _ := NewMCPToolNamespace(key, "", source, nil)
	set, _ := NewToolSet([]ToolDeclaration{namespace})
	item, _ := NewToolDeclarationsItem(set, ContextScopeRequest)
	if retained := RetainedHistory([]CanonicalItem{item}); len(retained) != 0 {
		t.Fatalf("request-scoped remote namespace survived: %#v", retained)
	}
}

func TestRemoteMCPSourceEquivalenceIncludesSelection(t *testing.T) {
	left, _ := NewMCPSource("https://mcp.example.test/rpc", Specify([]string{"search"}))
	same, _ := NewMCPSource("https://mcp.example.test/rpc", Specify([]string{"search"}))
	different, _ := NewMCPSource("https://mcp.example.test/rpc", Specify([]string{"fetch"}))
	if !left.Equivalent(same) {
		t.Fatal("equivalent MCP sources differ")
	}
	if left.Equivalent(different) {
		t.Fatal("different MCP selection was treated as equivalent")
	}
}

func TestRemoteNamespaceCannotBeNested(t *testing.T) {
	childKey, _ := NewToolKey("mcp", ToolKindNamespace, "docs")
	source, _ := NewMCPSource("https://mcp.example.test/rpc", Unspecified[[]string]())
	child, err := NewMCPToolNamespace(childKey, "", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	parentKey, _ := NewRequestToolKey(ToolKindNamespace, "parent")
	if _, err := NewToolNamespace(parentKey, "", []ToolDeclaration{child}); err == nil {
		t.Fatal("nested remote namespace entered canonical state")
	}
}
