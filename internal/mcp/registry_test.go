package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestWorkspaceCatalogResolvesOnlyDeclaredSourceBindings(t *testing.T) {
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(object)
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	toolKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	tool, _ := canonical.NewFunctionTool(toolKey, "search", schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	declaration, _ := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{tool})
	source, _ := declaration.MCP()
	slug, _ := routing.ParseWorkspaceSlug("research")
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"docs": {ConnectionID: "source-docs", AllowedTools: []string{"search"}}})
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]SourceBinding{"source-docs": {ID: "source-docs", ConnectionID: "source-docs", ConnectionRevision: "2", Source: source, Call: func(context.Context, string, json.RawMessage) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{}, nil
	}}}
	native, err := freezeNativeTool(&sdk.Tool{Name: "search", Title: "native title", InputSchema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatal(err)
	}
	binding := bindings["source-docs"]
	binding.NativeTools = map[string]json.RawMessage{"search": native}
	bindings["source-docs"] = binding
	catalog, err := WorkspaceCatalog(workspace, bindings)
	if err != nil || len(catalog) != 1 || catalog[0].Name != "docs__search" || catalog[0].SourceID != "source-docs" {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
	if catalog[0].ConnectionID != "source-docs" || catalog[0].ConnectionRevision != "2" {
		t.Fatalf("catalog lost Connection identity: %#v", catalog[0])
	}
	if _, err := CallWorkspaceTool(context.Background(), workspace, bindings, catalog[0], json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	changed, err := freezeNativeTool(&sdk.Tool{Name: "search", Title: "changed native title", InputSchema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatal(err)
	}
	binding.NativeTools["search"] = changed
	if _, err := CallWorkspaceTool(context.Background(), workspace, bindings, catalog[0], json.RawMessage(`{}`)); err == nil {
		t.Fatal("native descriptor drift dispatched frozen binding")
	}
	if string(catalog[0].NativeDescriptor) != string(native) {
		t.Fatal("binding mutation changed frozen catalog")
	}
	binding.NativeTools["search"] = native
	projected, err := ProjectSelectedDeclarations(catalog, []string{"docs__search"})
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := canonical.NewToolCallID("call_selected")
	input, _ := canonical.ParseJSONObject([]byte(`{"query":"docs"}`))
	item, err := canonical.NewToolCallItem(callID, projected[0].Key(), canonical.NewJSONObjectToolInput(input))
	if err != nil {
		t.Fatal(err)
	}
	call, _ := item.ToolCall()
	calls := 0
	bindings["source-docs"] = SourceBinding{ID: "source-docs", ConnectionID: "source-docs", ConnectionRevision: "2", Source: source, NativeTools: binding.NativeTools, Call: func(ctx context.Context, name string, args json.RawMessage) (*sdk.CallToolResult, error) {
		calls++
		if _, bounded := ctx.Deadline(); !bounded {
			t.Fatal("Workspace effect has no runtime deadline")
		}
		if name != "search" || string(args) != `{"query":"docs"}` {
			t.Fatalf("dispatch name=%q args=%s", name, args)
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "found"}}, IsError: true}, nil
	}}
	resultItem, err := CallWorkspaceToolResult(t.Context(), workspace, bindings, catalog[0], call)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := resultItem.ToolResult()
	if !ok || result.CallID() != callID || !result.IsError() || calls != 1 {
		t.Fatalf("causal result=%#v calls=%d", result, calls)
	}
	clientKey, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "docs__search")
	clientItem, _ := canonical.NewToolCallItem(callID, clientKey, canonical.NewJSONObjectToolInput(input))
	clientCall, _ := clientItem.ToolCall()
	if _, err := CallWorkspaceToolResult(t.Context(), workspace, bindings, catalog[0], clientCall); err == nil || calls != 1 {
		t.Fatal("client-owned identity dispatched")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := CallWorkspaceToolResult(cancelled, workspace, bindings, catalog[0], call); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled call dispatched")
	}
	// Identical published names and source labels cannot authorize rebinding.
	replacement, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"docs": {ConnectionID: "replacement", AllowedTools: []string{"search"}}})
	if err != nil {
		t.Fatal(err)
	}
	bindings["replacement"] = SourceBinding{ID: "replacement", Source: source, Call: func(context.Context, string, json.RawMessage) (*sdk.CallToolResult, error) {
		t.Fatal("frozen name dispatched to replacement source")
		return nil, nil
	}}
	if _, err := CallWorkspaceTool(context.Background(), replacement, bindings, catalog[0], json.RawMessage(`{}`)); err == nil {
		t.Fatal("changed source binding accepted")
	}
	if _, err := CallWorkspaceToolResult(t.Context(), replacement, bindings, catalog[0], call); err == nil || calls != 1 {
		t.Fatal("canonical call dispatched after source rebinding")
	}
	changedTool, _ := canonical.NewFunctionTool(toolKey, "changed description", schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	changedDeclaration, _ := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{changedTool})
	changedSource, _ := changedDeclaration.MCP()
	bindings["source-docs"] = SourceBinding{ID: "source-docs", Source: changedSource, Call: func(context.Context, string, json.RawMessage) (*sdk.CallToolResult, error) {
		t.Fatal("frozen name dispatched with changed declaration")
		return nil, nil
	}}
	if _, err := CallWorkspaceTool(context.Background(), workspace, bindings, catalog[0], json.RawMessage(`{}`)); err == nil {
		t.Fatal("changed declaration accepted")
	}
	if _, err := CallWorkspaceToolResult(t.Context(), workspace, bindings, catalog[0], call); err == nil || calls != 1 {
		t.Fatal("canonical call dispatched after schema/declaration change")
	}
	delete(bindings, "source-docs")
	if _, err := WorkspaceCatalog(workspace, bindings); err == nil {
		t.Fatal("unknown source binding was accepted")
	}
}

func TestConnectionCatalogProjectsRevisionBoundDeclarations(t *testing.T) {
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(object)
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	toolKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	tool, _ := canonical.NewFunctionTool(toolKey, "search", schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	declaration, _ := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{tool})
	source, _ := declaration.MCP()
	catalog, err := ConnectionCatalog("cn_mcp", map[string]SourceBinding{
		"cn_mcp": {ID: "cn_mcp", ConnectionID: "cn_mcp", ConnectionRevision: "4", Source: source, Call: func(context.Context, string, json.RawMessage) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{}, nil
		}},
	})
	if err != nil || len(catalog) != 1 {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
	if catalog[0].Name != "search" || catalog[0].RemoteName != "search" || catalog[0].ConnectionRevision != "4" {
		t.Fatalf("catalog lost Connection projection: %#v", catalog[0])
	}
}

func TestConnectionCatalogRejectsUnversionedImportedBinding(t *testing.T) {
	if _, err := ConnectionCatalog("imported", map[string]SourceBinding{
		"imported": {ID: "imported", Call: func(context.Context, string, json.RawMessage) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{}, nil
		}},
	}); err == nil {
		t.Fatal("imported Connection bypassed revision admission")
	}
}

func TestWorkspaceCatalogRejectsUnversionedConnectionBinding(t *testing.T) {
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(object)
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "cn_mcp")
	toolKey, _ := canonical.NewToolKey("mcp/cn_mcp", canonical.ToolKindFunction, "search")
	tool, _ := canonical.NewFunctionTool(toolKey, "search", schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	declaration, _ := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{tool})
	source, _ := declaration.MCP()
	slug, _ := routing.ParseWorkspaceSlug("research")
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"docs": {ConnectionID: "cn_mcp", AllowedTools: []string{"search"}}})
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]SourceBinding{"cn_mcp": {ID: "cn_mcp", Source: source, Call: func(context.Context, string, json.RawMessage) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{}, nil
	}}}
	if _, err := WorkspaceCatalog(workspace, bindings); err == nil {
		t.Fatal("unversioned Connection binding was accepted")
	}
}

func TestSharedSourceBindingKeepsWorkspaceMountDisclosureIsolated(t *testing.T) {
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(object)
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "shared")
	searchKey, _ := canonical.NewToolKey("mcp/shared", canonical.ToolKindFunction, "search")
	adminKey, _ := canonical.NewToolKey("mcp/shared", canonical.ToolKindFunction, "admin")
	search, _ := canonical.NewFunctionTool(searchKey, "search", schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	admin, _ := canonical.NewFunctionTool(adminKey, "admin", schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	declaration, _ := canonical.NewMCPToolSource(sourceKey, "shared source", canonical.MCPSource{}, []canonical.ToolDeclaration{search, admin})
	source, _ := declaration.MCP()
	firstSlug, _ := routing.ParseWorkspaceSlug("first")
	secondSlug, _ := routing.ParseWorkspaceSlug("second")
	first, err := routing.RestoreWorkspaceWithMCPMounts(firstSlug, nil, nil, map[string]routing.MCPMount{
		"docs": {ConnectionID: "shared-source", AllowedTools: []string{"search"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := routing.RestoreWorkspaceWithMCPMounts(secondSlug, nil, nil, map[string]routing.MCPMount{
		"ops": {ConnectionID: "shared-source", AllowedTools: []string{"admin"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := make([]string, 0, 2)
	bindings := map[string]SourceBinding{
		"shared-source": {
			ID: "shared-source", ConnectionID: "shared-source", ConnectionRevision: "1", Source: source,
			Call: func(_ context.Context, name string, _ json.RawMessage) (*sdk.CallToolResult, error) {
				calls = append(calls, name)
				return &sdk.CallToolResult{}, nil
			},
		},
	}
	firstCatalog, err := WorkspaceCatalog(first, bindings)
	if err != nil {
		t.Fatal(err)
	}
	secondCatalog, err := WorkspaceCatalog(second, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstCatalog) != 1 || firstCatalog[0].Name != "docs__search" {
		t.Fatalf("first Workspace disclosed unexpected tools: %#v", firstCatalog)
	}
	if len(secondCatalog) != 1 || secondCatalog[0].Name != "ops__admin" {
		t.Fatalf("second Workspace disclosed unexpected tools: %#v", secondCatalog)
	}
	if _, err := CallWorkspaceTool(context.Background(), first, bindings, firstCatalog[0], json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := CallWorkspaceTool(context.Background(), second, bindings, secondCatalog[0], json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"search", "admin"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("shared binding dispatches = %#v, want %#v", calls, want)
	}
	if _, err := CallWorkspaceTool(context.Background(), first, bindings, secondCatalog[0], json.RawMessage(`{}`)); err == nil {
		t.Fatal("catalog entry from another Workspace was accepted")
	}
}
