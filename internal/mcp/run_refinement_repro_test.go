package mcp

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestAttemptRequestPreservesDeferredFunctionWithLocalMCPSource(t *testing.T) {
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(schemaObject)

	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	remoteA, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	remoteB, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "fetch")
	deferredKey, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")

	remoteADecl, _ := canonical.NewFunctionTool(remoteA, "", schema, canonical.Unspecified[bool]())
	remoteBDecl, _ := canonical.NewFunctionTool(remoteB, "", schema, canonical.Unspecified[bool]())
	deferredDecl, _ := canonical.NewFunctionTool(deferredKey, "", schema, canonical.Unspecified[bool]())

	remote, _ := newTestMCPURL("https://mcp.example.test/rpc", canonical.Unspecified[[]string]())
	// One local MCP source catalog that resolves to two function declarations.
	mcpSource, _ := canonical.NewMCPToolSource(sourceKey, "", remote, []canonical.ToolDeclaration{remoteADecl, remoteBDecl})

	// One occurrence holding [mcpSource, deferred ordinary function].
	occurrence, _ := canonical.NewToolSet([]canonical.ToolDeclaration{mcpSource, deferredDecl})

	// The deferred refinement names the ordinary function, not the MCP source.
	refinement, err := canonical.NewResponsesToolRefinements(occurrence, []canonical.ToolKey{deferredKey})
	if err != nil {
		t.Fatalf("refinement construction = %v", err)
	}
	item, err := canonical.NewToolDeclarationsItemWithResponses(occurrence, canonical.ContextScopeRequest, refinement)
	if err != nil {
		t.Fatalf("item construction = %v", err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}})

	// A fully opened runtime: the source is local and expands to its two tools.
	run := &Run{
		sessions:     map[canonical.ToolKey]*session{sourceKey: {}},
		localSources: map[canonical.ToolKey]struct{}{sourceKey: {}},
		attemptTools: map[canonical.ToolKey][]canonical.ToolDeclaration{
			sourceKey: {remoteADecl.Clone(), remoteBDecl.Clone()},
		},
		bindings: map[canonical.ToolKey]binding{
			remoteA: {source: sourceKey, remoteName: remoteA.Name()},
			remoteB: {source: sourceKey, remoteName: remoteB.Name()},
		},
	}

	attempt, err := run.AttemptRequest(request)
	if err != nil {
		t.Fatalf("AttemptRequest = %v", err)
	}

	// Sanity: the deferred refinement must survive on the preserved function.
	occ, ok := attempt.Items()[0].ToolDeclarations()
	if !ok {
		t.Fatalf("declaration occurrence missing after rewrite")
	}
	if !occ.Responses().Deferred(deferredKey) {
		t.Fatalf("deferred refinement for %q was lost even though the rewrite succeeded", deferredKey.String())
	}
}
