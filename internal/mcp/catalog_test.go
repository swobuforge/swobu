package mcp

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestBuildCatalogRejectsInvalidAliasesAndPublishedNames(t *testing.T) {
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	for _, testCase := range []struct{ alias, remote string }{
		{"Docs", "search"}, {"1docs", "search"}, {" docs", "search"}, {"docs/extra", "search"}, {strings.Repeat("a", 33), "search"}, {"docs", strings.Repeat("a", 123)},
	} {
		t.Run(testCase.alias+"/"+testCase.remote, func(t *testing.T) {
			key, err := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, testCase.remote)
			if err != nil {
				t.Fatal(err)
			}
			tool, err := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(object), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
			if err != nil {
				t.Fatal(err)
			}
			declaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{tool})
			if err != nil {
				t.Fatal(err)
			}
			source, _ := declaration.MCP()
			if _, err := BuildCatalog([]Mount{{Alias: testCase.alias, Source: source, AllowTools: []string{testCase.remote}}}); err == nil {
				t.Fatal("invalid published catalog accepted")
			}
		})
	}
}

func TestBuildCatalogPreservesMaximumLengthPublishedName(t *testing.T) {
	object, err := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	remote := strings.Repeat("a", 122)
	key, err := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, remote)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(object), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	if err != nil {
		t.Fatal(err)
	}
	sourceKey, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{tool})
	if err != nil {
		t.Fatal(err)
	}
	source, _ := declaration.MCP()
	catalog, err := BuildCatalog([]Mount{{Alias: "docs", Source: source, AllowTools: []string{remote}}})
	if err != nil || len(catalog) != 1 || catalog[0].Name != "docs__"+remote || catalog[0].RemoteName != remote {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
}

func TestBuildCatalogNamespacesSelectedToolsDeterministically(t *testing.T) {
	object, err := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	schema := canonical.NewToolSchemaObject(object)
	makeSource := func(alias string) canonical.MCPToolSource {
		sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, alias)
		searchKey, _ := canonical.NewToolKey("mcp/"+alias, canonical.ToolKindFunction, "search")
		fetchKey, _ := canonical.NewToolKey("mcp/"+alias, canonical.ToolKindFunction, "fetch")
		search, _ := canonical.NewFunctionTool(searchKey, "search", schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
		fetch, _ := canonical.NewFunctionTool(fetchKey, "fetch", schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
		decl, _ := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{search, fetch})
		value, _ := decl.MCP()
		return value
	}
	tools, err := BuildCatalog([]Mount{{Alias: "docs", Source: makeSource("docs"), AllowTools: []string{"search"}}, {Alias: "github", Source: makeSource("github"), AllowTools: []string{"search"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name != "docs__search" || tools[1].Name != "github__search" {
		t.Fatalf("tools=%#v", tools)
	}
	if _, err := BuildCatalog([]Mount{{Alias: "docs", Source: makeSource("docs"), AllowTools: []string{"*"}}}); err == nil {
		t.Fatal("wildcard allowlist accepted")
	}
	for _, allowed := range [][]string{{"missing"}, {"search", "missing"}, {"Search"}} {
		if catalog, err := BuildCatalog([]Mount{{Alias: "docs", Source: makeSource("docs"), AllowTools: allowed}}); err == nil || catalog != nil {
			t.Fatalf("unresolved mount %v silently published partial catalog: %#v, %v", allowed, catalog, err)
		}
	}
}

func TestSelectCatalogIsExplicitAndFailsClosed(t *testing.T) {
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(object)
	makeTool := func(name string) CatalogTool {
		key, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, name)
		decl, _ := canonical.NewFunctionTool(key, name, schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
		return CatalogTool{Name: "docs__" + name, RemoteName: name, Declaration: decl}
	}
	catalog := []CatalogTool{makeTool("fetch"), makeTool("search")}
	selected, err := SelectCatalog(catalog, []string{"docs__search"})
	if err != nil || len(selected) != 1 || selected[0].Name != "docs__search" {
		t.Fatalf("selected=%#v err=%v", selected, err)
	}
	if _, err := SelectCatalog(catalog, []string{"docs__delete"}); err == nil {
		t.Fatal("unpublished tool selected")
	}
	if _, err := SelectCatalog(catalog, []string{"docs__search", "docs__search"}); err == nil {
		t.Fatal("duplicate selection accepted")
	}
	declarations, err := ProjectSelectedDeclarations(catalog, []string{"docs__search"})
	if err != nil || len(declarations) != 1 || declarations[0].Key().Name() != "docs__search" {
		t.Fatalf("declarations=%#v err=%v", declarations, err)
	}
}
