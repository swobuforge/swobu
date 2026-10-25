package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestRequestOwnedPreparedCallPreservesNumericArgumentsBeforeSourceEffect(t *testing.T) {
	ctx := t.Context()
	upstream := sdk.NewServer(&sdk.Implementation{Name: "numeric-source", Version: "1"}, nil)
	var received atomic.Value
	upstream.AddTool(&sdk.Tool{Name: "fetch", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(_ context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		received.Store(string(request.Params.Arguments))
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "done"}}}, nil
	})
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := upstream.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "numeric-client", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	sourceDeclaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := sourceDeclaration.MCP()
	toolKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "fetch")
	run := &Run{sessions: map[canonical.ToolKey]*session{sourceKey: {source: source, sdk: clientSession}}, bindings: map[canonical.ToolKey]binding{toolKey: {source: sourceKey, remoteName: "fetch"}}}
	raw := `{"nested":{"integer":9007199254740993,"decimal":0.12345678901234567890123456789},"array":[9007199254740995]}`
	input, err := canonical.ParseJSONObject([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := canonical.NewToolCallID("numeric-call")
	call, err := canonical.NewToolCallItem(callID, toolKey, canonical.NewJSONObjectToolInput(input))
	if err != nil {
		t.Fatal(err)
	}
	toolCall, _ := call.ToolCall()
	prepared, err := run.PrepareCall(toolCall)
	if err != nil || received.Load() != nil {
		t.Fatalf("preflight executed source or failed: %v", err)
	}
	if _, err := prepared.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	actual, err := canonical.ParseJSONObject([]byte(received.Load().(string)))
	if err != nil {
		t.Fatal(err)
	}
	if actual.String() != input.String() {
		t.Fatalf("prepared arguments changed before source effect: got=%s want=%s", received.Load(), raw)
	}
}

func TestTwoMountedSourcesKeepNativeSearchSchemasAndDispatchIdentity(t *testing.T) {
	ctx := t.Context()
	type sourceFixture struct {
		binding SourceBinding
		calls   *atomic.Int32
	}
	makeSource := func(id string, inputSchema, outputSchema string) sourceFixture {
		t.Helper()
		calls := &atomic.Int32{}
		upstream := sdk.NewServer(&sdk.Implementation{Name: id + "-source", Version: "1"}, nil)
		upstream.AddTool(&sdk.Tool{
			Name: "search", Description: "Search " + id,
			InputSchema: json.RawMessage(inputSchema), OutputSchema: json.RawMessage(outputSchema),
		}, func(_ context.Context, _ *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			calls.Add(1)
			return &sdk.CallToolResult{StructuredContent: map[string]any{"source": id}}, nil
		})
		serverTransport, clientTransport := sdk.NewInMemoryTransports()
		serverSession, err := upstream.Connect(ctx, serverTransport, nil)
		if err != nil {
			t.Fatal(err)
		}
		client := sdk.NewClient(&sdk.Implementation{Name: "source-client", Version: "1"}, nil)
		sourceSession, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			_ = serverSession.Close()
			t.Fatal(err)
		}
		sourceKey, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, id)
		if err != nil {
			t.Fatal(err)
		}
		declaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, nil)
		if err != nil {
			_ = sourceSession.Close()
			_ = serverSession.Close()
			t.Fatal(err)
		}
		source, _ := declaration.MCP()
		binding, err := sourceBindingFromSession(ctx, &session{source: source, sdk: sourceSession})
		if err != nil {
			_ = serverSession.Close()
			t.Fatal(err)
		}
		binding.ConnectionID = id
		binding.ConnectionRevision = "1"
		binding.Close = func() error { return errors.Join(sourceSession.Close(), serverSession.Close()) }
		t.Cleanup(func() { _ = binding.Close() })
		return sourceFixture{binding: binding, calls: calls}
	}

	docs := makeSource("docs", `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`, `{"type":"object","properties":{"documents":{"type":"array"}}}`)
	github := makeSource("github", `{"type":"object","properties":{"issue":{"type":"integer"}},"required":["issue"]}`, `{"type":"object","properties":{"issues":{"type":"array"}}}`)
	slug, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{
		"docs":   {ConnectionID: "docs", AllowedTools: []string{"search"}},
		"github": {ConnectionID: "github", AllowedTools: []string{"search"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]SourceBinding{"docs": docs.binding, "github": github.binding}
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(_ *http.Request, name string) bool { return name == "research" },
		Catalog: func(_ context.Context, name string) ([]CatalogTool, error) {
			if name != "research" {
				return nil, errors.New("unexpected Workspace")
			}
			return WorkspaceCatalog(workspace, bindings)
		},
		Call: func(callCtx context.Context, name string, tool CatalogTool, arguments json.RawMessage) (*sdk.CallToolResult, error) {
			if name != "research" {
				return nil, errors.New("unexpected Workspace")
			}
			return CallWorkspaceTool(callCtx, workspace, bindings, tool, arguments)
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "workspace-client", Version: "1"}, nil)
	workspaceSession, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: server.URL + "/workspaces/research/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceSession.Close()
	listed, err := workspaceSession.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 2 || listed.Tools[0].Name != "docs__search" || listed.Tools[1].Name != "github__search" {
		t.Fatalf("namespaced catalog=%#v err=%v", listed, err)
	}
	for _, expected := range []struct {
		name, input, output string
	}{
		{"docs__search", `{"properties":{"query":{"type":"string"}},"required":["query"],"type":"object"}`, `{"properties":{"documents":{"type":"array"}},"type":"object"}`},
		{"github__search", `{"properties":{"issue":{"type":"integer"}},"required":["issue"],"type":"object"}`, `{"properties":{"issues":{"type":"array"}},"type":"object"}`},
	} {
		var got *sdk.Tool
		for _, tool := range listed.Tools {
			if tool.Name == expected.name {
				got = tool
			}
		}
		if got == nil {
			t.Fatalf("native schema changed for %s: %#v", expected.name, got)
		}
		input, inputErr := json.Marshal(got.InputSchema)
		output, outputErr := json.Marshal(got.OutputSchema)
		if inputErr != nil || outputErr != nil || string(input) != expected.input || string(output) != expected.output {
			t.Fatalf("native schema changed for %s: input=%s output=%s errors=(%v,%v)", expected.name, input, output, inputErr, outputErr)
		}
	}
	for _, expected := range []struct {
		name string
		args map[string]any
		want int32
	}{
		{"docs__search", map[string]any{"query": "manual"}, 1},
		{"github__search", map[string]any{"issue": 42}, 1},
	} {
		result, err := workspaceSession.CallTool(ctx, &sdk.CallToolParams{Name: expected.name, Arguments: expected.args})
		if err != nil || result == nil {
			t.Fatalf("call %s result=%#v err=%v", expected.name, result, err)
		}
		structured, ok := result.StructuredContent.(map[string]any)
		if !ok || structured["source"] == nil {
			t.Fatalf("call %s lost native structured content: %#v", expected.name, result.StructuredContent)
		}
		if expected.name == "docs__search" && (docs.calls.Load() != expected.want || github.calls.Load() != 0 || structured["source"] != "docs") {
			t.Fatalf("docs dispatch crossed source boundary: result=%#v docs=%d github=%d", result, docs.calls.Load(), github.calls.Load())
		}
		if expected.name == "github__search" && (github.calls.Load() != expected.want || docs.calls.Load() != expected.want || structured["source"] != "github") {
			t.Fatalf("github dispatch crossed source boundary: result=%#v docs=%d github=%d", result, docs.calls.Load(), github.calls.Load())
		}
	}
}

func TestPublicWorkspaceCallRejectsNativeSchemaChangeBeforeSourceDispatch(t *testing.T) {
	ctx := t.Context()
	var sourceCalls atomic.Int32
	upstream := sdk.NewServer(&sdk.Implementation{Name: "docs-source", Version: "1"}, nil)
	upstream.AddTool(&sdk.Tool{
		Name:         "search",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"documents":{"type":"array"}}}`),
	}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		sourceCalls.Add(1)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "found"}}}, nil
	})
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	upstreamSession, err := upstream.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer upstreamSession.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "source-client", Version: "1"}, nil)
	sourceSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceSession.Close()
	sourceKey, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	if err != nil {
		t.Fatal(err)
	}
	sourceDeclaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := sourceDeclaration.MCP()
	binding, err := sourceBindingFromSession(ctx, &session{source: source, sdk: sourceSession})
	if err != nil {
		t.Fatal(err)
	}
	binding.ConnectionID, binding.ConnectionRevision = "docs", "1"
	slug, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{
		"docs": {ConnectionID: "docs", AllowedTools: []string{"search"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]SourceBinding{"docs": binding}
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(_ *http.Request, name string) bool { return name == "research" },
		Catalog: func(_ context.Context, name string) ([]CatalogTool, error) {
			if name != "research" {
				return nil, errors.New("unexpected Workspace")
			}
			return WorkspaceCatalog(workspace, bindings)
		},
		Call: func(callCtx context.Context, name string, selected CatalogTool, arguments json.RawMessage) (*sdk.CallToolResult, error) {
			if name != "research" {
				return nil, errors.New("unexpected Workspace")
			}
			// Model the source descriptor changing after the request has selected
			// its frozen catalog entry but before any remote effect can begin.
			changed := bindings["docs"]
			native, freezeErr := freezeNativeTool(&sdk.Tool{
				Name:         "search",
				InputSchema:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
				OutputSchema: json.RawMessage(`{"type":"object","properties":{"deleted":{"type":"boolean"}}}`),
			})
			if freezeErr != nil {
				return nil, freezeErr
			}
			changed.NativeTools["search"] = native
			bindings["docs"] = changed
			return CallWorkspaceTool(callCtx, workspace, bindings, selected, arguments)
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	workspaceClient := sdk.NewClient(&sdk.Implementation{Name: "workspace-client", Version: "1"}, nil)
	workspaceSession, err := workspaceClient.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: server.URL + "/workspaces/research/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceSession.Close()
	listed, err := workspaceSession.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "docs__search" {
		t.Fatalf("listed native catalog=%#v err=%v", listed, err)
	}
	if _, err := workspaceSession.CallTool(ctx, &sdk.CallToolParams{Name: listed.Tools[0].Name, Arguments: map[string]any{"query": "manual"}}); err == nil || sourceCalls.Load() != 0 {
		t.Fatalf("changed native descriptor rebound a listed call: source calls=%d err=%v", sourceCalls.Load(), err)
	}
}

func TestSDKDiscoveryToWorkspacePreservesNativeDescriptorAndResult(t *testing.T) {
	ctx := t.Context()
	upstream := sdk.NewServer(&sdk.Implementation{Name: "native-source", Version: "1"}, &sdk.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}})
	native := &sdk.Tool{Name: "fetch", Title: "Native Fetch", Description: "Untrusted source description", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`), OutputSchema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"integer"}}}`), Annotations: &sdk.ToolAnnotations{Title: "Native annotation", ReadOnlyHint: true}}
	native.Meta = sdk.Meta{"source.example/display": map[string]any{"label": "untrusted"}}
	native.Icons = []sdk.Icon{{Source: "https://source.example/icon.png", MIMEType: "image/png", Sizes: []string{"32x32"}}}
	nativeResult := &sdk.CallToolResult{
		Meta: sdk.Meta{sdk.MetaKeyServerInfo: &sdk.Implementation{Name: "native-source", Version: "1"}, "source.example/result": "untrusted"},
		Content: []sdk.Content{
			&sdk.TextContent{Text: "native result", Meta: sdk.Meta{"source.example/content": "untrusted"}, Annotations: &sdk.Annotations{Audience: []sdk.Role{"user", "assistant"}, Priority: 0.7}},
			&sdk.ImageContent{Data: []byte{1, 2, 3}, MIMEType: "image/png"},
			&sdk.AudioContent{Data: []byte{4, 5, 6}, MIMEType: "audio/wav"},
			&sdk.ResourceLink{URI: "https://source.example/document", Name: "document", Title: "Native document", MIMEType: "text/plain"},
			&sdk.EmbeddedResource{Resource: &sdk.ResourceContents{URI: "source://document", MIMEType: "text/plain", Text: "embedded document"}},
		},
		StructuredContent: map[string]any{"answer": float64(7)},
	}
	var calls atomic.Int32
	var receivedArguments atomic.Value
	upstream.AddTool(native, func(_ context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		calls.Add(1)
		receivedArguments.Store(string(request.Params.Arguments))
		if string(request.Params.Arguments) == `{"query":"error"}` {
			return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "source rejected query"}}, Meta: sdk.Meta{"source.example/error": "untrusted"}}, nil
		}
		return nativeResult, nil
	})
	upstream.AddTool(&sdk.Tool{Name: "hidden", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		calls.Add(1)
		return &sdk.CallToolResult{}, nil
	})
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	upstreamSession, err := upstream.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer upstreamSession.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "source-client", Version: "1"}, nil)
	sourceSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceSession.Close()
	key, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	sourceDeclaration, err := canonical.NewMCPToolSource(key, "", canonical.MCPSource{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := sourceDeclaration.MCP()
	binding, err := sourceBindingFromSession(ctx, &session{source: source, sdk: sourceSession})
	if err != nil {
		t.Fatal(err)
	}
	binding.ConnectionID, binding.ConnectionRevision = "docs", "1"
	slug, _ := routing.ParseWorkspaceSlug("research")
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"docs": {ConnectionID: "docs", AllowedTools: []string{"fetch"}}})
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]SourceBinding{"docs": binding}
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(_ *http.Request, name string) bool { return name == "research" },
		Catalog:   func(context.Context, string) ([]CatalogTool, error) { return WorkspaceCatalog(workspace, bindings) },
		Call: func(ctx context.Context, _ string, tool CatalogTool, arguments json.RawMessage) (*sdk.CallToolResult, error) {
			return CallWorkspaceTool(ctx, workspace, bindings, tool, arguments)
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	external := sdk.NewClient(&sdk.Implementation{Name: "workspace-client", Version: "1"}, nil)
	workspaceSession, err := external.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: server.URL + "/workspaces/research/mcp", DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceSession.Close()
	listed, err := workspaceSession.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 1 {
		t.Fatalf("Workspace catalog=%#v, %v", listed, err)
	}
	var expected sdk.Tool
	originalDescriptor, err := json.Marshal(native)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(originalDescriptor, &expected); err != nil {
		t.Fatal(err)
	}
	expected.Name = "docs__fetch"
	if !reflect.DeepEqual(listed.Tools[0], &expected) {
		t.Fatalf("discovery/publication lost native metadata: %#v", listed.Tools[0])
	}
	result, err := workspaceSession.CallTool(ctx, &sdk.CallToolParams{Name: "docs__fetch", Arguments: map[string]any{"query": "docs"}})
	serverInfo, serverInfoOK := result.Meta[sdk.MetaKeyServerInfo].(map[string]any)
	if err != nil || !serverInfoOK || serverInfo["name"] != "native-source" || serverInfo["version"] != "1" ||
		result.Meta["source.example/result"] != "untrusted" || !reflect.DeepEqual(result.Content, nativeResult.Content) ||
		!reflect.DeepEqual(result.StructuredContent, nativeResult.StructuredContent) || result.IsError != nativeResult.IsError || calls.Load() != 1 {
		t.Fatalf("native result/dispatch=%#v calls=%d err=%v", result, calls.Load(), err)
	}
	exactArguments := json.RawMessage(`{"query":"numeric","nested":{"integer":9007199254740993,"decimal":0.12345678901234567890123456789}}`)
	if _, err := workspaceSession.CallTool(ctx, &sdk.CallToolParams{Name: "docs__fetch", Arguments: exactArguments}); err != nil {
		t.Fatal(err)
	}
	var received, expectedArguments map[string]json.RawMessage
	if err := json.Unmarshal([]byte(receivedArguments.Load().(string)), &received); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(exactArguments, &expectedArguments); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(received, expectedArguments) {
		t.Fatalf("native arguments changed before source effect: got=%s want=%s", receivedArguments.Load(), exactArguments)
	}
	errorResult, err := workspaceSession.CallTool(ctx, &sdk.CallToolParams{Name: "docs__fetch", Arguments: map[string]any{"query": "error"}})
	errorServerInfo, serverInfoOK := errorResult.Meta[sdk.MetaKeyServerInfo].(map[string]any)
	if err != nil || !serverInfoOK || errorServerInfo["name"] != "swobu-workspace" || errorServerInfo["version"] != "v1" ||
		errorResult.Meta["source.example/error"] != "untrusted" || !errorResult.IsError || len(errorResult.Content) != 1 ||
		errorResult.Content[0].(*sdk.TextContent).Text != "source rejected query" || calls.Load() != 3 {
		t.Fatalf("native tool error became transport error: result=%#v calls=%d err=%v", errorResult, calls.Load(), err)
	}
	for _, rejected := range []string{"docs__hidden", "hidden", "fetch", "other__fetch", "swobu__manage_fetch"} {
		if _, err := workspaceSession.CallTool(ctx, &sdk.CallToolParams{Name: rejected, Arguments: map[string]any{}}); err == nil || calls.Load() != 3 {
			t.Fatalf("unpublished native tool %q dispatched: calls=%d err=%v", rejected, calls.Load(), err)
		}
	}
	frozenCatalog, err := WorkspaceCatalog(workspace, bindings)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := freezeNativeTool(&sdk.Tool{
		Name: "fetch", Title: "Changed source title",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"replacement":{"type":"string"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"replacement":{"type":"boolean"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	binding.NativeTools["fetch"] = changed
	if _, err := CallWorkspaceTool(ctx, workspace, bindings, frozenCatalog[0], json.RawMessage(`{"query":"docs"}`)); err == nil || calls.Load() != 3 {
		t.Fatalf("stale native descriptor dispatched: calls=%d err=%v", calls.Load(), err)
	}
	replacement, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"docs": {ConnectionID: "docs", AllowedTools: []string{"fetch", "removed"}}})
	if err != nil {
		t.Fatal(err)
	}
	workspace = replacement
	if _, err := workspaceSession.ListTools(ctx, nil); err == nil || calls.Load() != 3 {
		t.Fatalf("unresolved mount served partial catalog: calls=%d err=%v", calls.Load(), err)
	}
}

func TestNewUpstreamToolStaysHiddenAfterWorkspaceBindingRefresh(t *testing.T) {
	ctx := t.Context()
	var searchCalls atomic.Int32
	var deleteCalls atomic.Int32
	makeBinding := func(includeNewTool bool) SourceBinding {
		upstream := sdk.NewServer(&sdk.Implementation{Name: "changing-source", Version: "1"}, nil)
		upstream.AddTool(&sdk.Tool{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			searchCalls.Add(1)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "found"}}}, nil
		})
		if includeNewTool {
			upstream.AddTool(&sdk.Tool{Name: "delete_everything", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				deleteCalls.Add(1)
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "deleted"}}}, nil
			})
		}
		serverTransport, clientTransport := sdk.NewInMemoryTransports()
		serverSession, err := upstream.Connect(ctx, serverTransport, nil)
		if err != nil {
			t.Fatal(err)
		}
		client := sdk.NewClient(&sdk.Implementation{Name: "source-client", Version: "1"}, nil)
		sourceSession, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			_ = serverSession.Close()
			t.Fatal(err)
		}
		sourceKey, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
		if err != nil {
			t.Fatal(err)
		}
		declaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, nil)
		if err != nil {
			_ = sourceSession.Close()
			_ = serverSession.Close()
			t.Fatal(err)
		}
		source, _ := declaration.MCP()
		binding, err := sourceBindingFromSession(ctx, &session{source: source, sdk: sourceSession})
		if err != nil {
			_ = serverSession.Close()
			t.Fatal(err)
		}
		binding.ConnectionID, binding.ConnectionRevision = "docs", "1"
		binding.Close = func() error {
			return errors.Join(sourceSession.Close(), serverSession.Close())
		}
		t.Cleanup(func() { _ = binding.Close() })
		return binding
	}

	slug, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{
		"docs": {ConnectionID: "docs", AllowedTools: []string{"search"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]SourceBinding{"docs": makeBinding(false)}
	initial, err := WorkspaceCatalog(workspace, bindings)
	if err != nil || len(initial) != 1 || initial[0].Name != "docs__search" {
		t.Fatalf("initial catalog=%#v err=%v", initial, err)
	}

	// Reopening the upstream binding observes the new declaration. The durable
	// Workspace allowlist remains the publication authority across that refresh.
	bindings["docs"] = makeBinding(true)
	refreshed, err := WorkspaceCatalog(workspace, bindings)
	if err != nil || len(refreshed) != 1 || refreshed[0].Name != "docs__search" {
		t.Fatalf("refreshed catalog published an unselected upstream tool: %#v err=%v", refreshed, err)
	}
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(_ *http.Request, name string) bool { return name == "research" },
		Catalog: func(_ context.Context, name string) ([]CatalogTool, error) {
			if name != "research" {
				return nil, errors.New("unexpected Workspace")
			}
			return WorkspaceCatalog(workspace, bindings)
		},
		Call: func(callCtx context.Context, name string, tool CatalogTool, arguments json.RawMessage) (*sdk.CallToolResult, error) {
			if name != "research" {
				return nil, errors.New("unexpected Workspace")
			}
			return CallWorkspaceTool(callCtx, workspace, bindings, tool, arguments)
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "workspace-client", Version: "1"}, nil)
	workspaceSession, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: server.URL + "/workspaces/research/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceSession.Close()
	listed, err := workspaceSession.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "docs__search" {
		t.Fatalf("refreshed public catalog=%#v err=%v", listed, err)
	}
	if _, err := workspaceSession.CallTool(ctx, &sdk.CallToolParams{Name: "docs__delete_everything", Arguments: map[string]any{}}); err == nil || deleteCalls.Load() != 0 {
		t.Fatalf("guessed new tool dispatched after binding refresh: delete calls=%d err=%v", deleteCalls.Load(), err)
	}
	if _, err := workspaceSession.CallTool(ctx, &sdk.CallToolParams{Name: "delete_everything", Arguments: map[string]any{}}); err == nil || deleteCalls.Load() != 0 {
		t.Fatalf("unqualified new tool name dispatched: delete calls=%d err=%v", deleteCalls.Load(), err)
	}
	if _, err := workspaceSession.CallTool(ctx, &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{}}); err != nil || searchCalls.Load() != 1 {
		t.Fatalf("explicitly mounted tool stopped working: search calls=%d err=%v", searchCalls.Load(), err)
	}
}
