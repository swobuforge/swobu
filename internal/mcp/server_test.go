package mcp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestStatelessWorkspaceCatalogPaginationPreservesEverySelectedNativeTool(t *testing.T) {
	const toolCount = sdk.DefaultPageSize + 1
	catalog := make([]CatalogTool, 0, toolCount)
	for index := range toolCount {
		name := fmt.Sprintf("fetch-%04d", index)
		native, err := freezeNativeTool(&sdk.Tool{Name: name, Title: "Native " + name, InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Meta: sdk.Meta{"source.example/identity": name}})
		if err != nil {
			t.Fatal(err)
		}
		catalog = append(catalog, CatalogTool{Name: "docs__" + name, RemoteName: name, NativeDescriptor: native})
	}
	var calls atomic.Int32
	var withdrawn atomic.Bool
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(*http.Request, string) bool { return true },
		Catalog: func(context.Context, string) ([]CatalogTool, error) {
			if withdrawn.Load() {
				return nil, nil
			}
			return catalog, nil
		},
		Call: func(_ context.Context, _ string, selected CatalogTool, _ json.RawMessage) (*sdk.CallToolResult, error) {
			calls.Add(1)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: selected.RemoteName}}}, nil
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "paged-client", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: server.URL + "/workspaces/research/mcp", DisableStandaloneSSE: true, MaxRetries: -1}, &sdk.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var listed []*sdk.Tool
	cursor := ""
	continuation := ""
	for page := 0; page < 2; page++ {
		result, err := session.ListTools(t.Context(), &sdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		listed = append(listed, result.Tools...)
		cursor = result.NextCursor
		if page == 0 && (len(result.Tools) != sdk.DefaultPageSize || cursor == "") {
			t.Fatalf("first page failed to expose continuation: tools=%d cursor=%q", len(result.Tools), cursor)
		}
		if page == 0 {
			continuation = cursor
		}
	}
	if len(listed) != toolCount || cursor != "" || calls.Load() != 0 {
		t.Fatalf("pagination lost tools or executed effects: tools=%d cursor=%q calls=%d", len(listed), cursor, calls.Load())
	}
	for index, tool := range listed {
		var expected sdk.Tool
		if err := json.Unmarshal(catalog[index].NativeDescriptor, &expected); err != nil {
			t.Fatal(err)
		}
		expected.Name = catalog[index].Name
		if !reflect.DeepEqual(tool, &expected) {
			t.Fatalf("paged native tool %d changed: got=%#v expected=%#v", index, tool, expected)
		}
	}
	result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: listed[toolCount-1].Name, Arguments: map[string]any{}})
	if err != nil || calls.Load() != 1 || len(result.Content) != 1 || result.Content[0].(*sdk.TextContent).Text != catalog[toolCount-1].RemoteName {
		t.Fatalf("last page tool misdispatched: calls=%d result=%#v err=%v", calls.Load(), result, err)
	}
	if _, err := session.ListTools(t.Context(), &sdk.ListToolsParams{Cursor: "not-a-valid-cursor"}); err == nil || calls.Load() != 1 {
		t.Fatalf("invalid cursor accepted or executed effects: calls=%d err=%v", calls.Load(), err)
	}
	withdrawn.Store(true)
	remaining, err := session.ListTools(t.Context(), &sdk.ListToolsParams{Cursor: continuation})
	if err != nil || len(remaining.Tools) != 0 || remaining.NextCursor != "" || calls.Load() != 1 {
		t.Fatalf("continuation retained withdrawn tools: %#v calls=%d err=%v", remaining, calls.Load(), err)
	}
	if _, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: listed[toolCount-1].Name, Arguments: map[string]any{}}); err == nil || calls.Load() != 1 {
		t.Fatalf("withdrawn last-page tool dispatched: calls=%d err=%v", calls.Load(), err)
	}
}

func TestWorkspaceCatalogIsFreshAcrossIndependentSDKClients(t *testing.T) {
	var revision atomic.Int32
	var calls atomic.Int32
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(*http.Request, string) bool { return true },
		Catalog: func(context.Context, string) ([]CatalogTool, error) {
			name := "first"
			if revision.Load() > 0 {
				name = "second"
			}
			native, err := freezeNativeTool(&sdk.Tool{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)})
			return []CatalogTool{{Name: "docs__" + name, RemoteName: name, NativeDescriptor: native}}, err
		},
		Call: func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error) {
			calls.Add(1)
			return &sdk.CallToolResult{}, nil
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, expected := range []string{"docs__first", "docs__second"} {
		client := sdk.NewClient(&sdk.Implementation{Name: "fresh-client", Version: "1"}, nil)
		session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: server.URL + "/workspaces/research/mcp"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		listed, err := session.ListTools(t.Context(), nil)
		if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != expected {
			t.Fatalf("fresh catalog expected %q, got %#v, %v", expected, listed, err)
		}
		revision.Add(1)
		refreshed, err := session.ListTools(t.Context(), nil)
		if err != nil || len(refreshed.Tools) != 1 || refreshed.Tools[0].Name != "docs__second" {
			t.Fatalf("existing client retained stale catalog: %#v, %v", refreshed, err)
		}
		if session.ID() != "" {
			t.Fatalf("stateless Workspace issued session identity %q", session.ID())
		}
		if _, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__first", Arguments: map[string]any{}}); err == nil || calls.Load() != 0 {
			t.Fatalf("removed tool dispatched: calls=%d err=%v", calls.Load(), err)
		}
	}
}

func TestWorkspaceMCPRejectsOversizedNativeResultMetadataWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	native, err := freezeNativeTool(&sdk.Tool{Name: "fetch", InputSchema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(*http.Request, string) bool { return true },
		Catalog: func(context.Context, string) ([]CatalogTool, error) {
			return []CatalogTool{{Name: "docs__fetch", RemoteName: "fetch", NativeDescriptor: native}}, nil
		},
		Call: func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error) {
			calls.Add(1)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "small"}}, Meta: sdk.Meta{"source.example/large": strings.Repeat("x", MaxToolResultBytes+1)}}, nil
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "bounded-client", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: server.URL + "/workspaces/research/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__fetch", Arguments: map[string]any{}}); err == nil || calls.Load() != 1 {
		t.Fatalf("oversized metadata delivered or effect retried: calls=%d err=%v", calls.Load(), err)
	}
}

func TestWorkspaceMCPResponsesNeverPermitSharedCaching(t *testing.T) {
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(request *http.Request, _ string) bool {
			return request.Header.Get("Authorization") != "Bearer rejected"
		},
		Catalog: func(context.Context, string) ([]CatalogTool, error) { return nil, nil },
		Call: func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error) {
			return nil, nil
		},
	})
	for _, testCase := range []struct {
		name   string
		path   string
		origin string
		body   string
		denied bool
	}{
		{"catalog", "/workspaces/research/mcp", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, false},
		{"unknown tool", "/workspaces/research/mcp", "", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hidden","arguments":{}}}`, false},
		{"hostile origin", "/workspaces/research/mcp", "https://evil.example", `{}`, false},
		{"ambiguous path", "/workspaces/research/mcp/extra", "", `{}`, false},
		{"denied caller", "/workspaces/research/mcp", "", `{}`, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, testCase.path, strings.NewReader(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			request.Header.Set("MCP-Protocol-Version", "2025-11-25")
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}
			if testCase.denied {
				request.Header.Set("Authorization", "Bearer rejected")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("permission-sensitive response allows caching: status=%d headers=%v", response.Code, response.Header())
			}
			if testCase.name == "catalog" && (response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"tools":[]`)) {
				t.Fatalf("cache proof did not reach catalog: status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestWorkspaceMCPHandlerListsAndCallsFrozenCatalogBinding(t *testing.T) {
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(object)
	key, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	declaration, _ := canonical.NewFunctionTool(key, "search", schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	native, err := freezeNativeTool(&sdk.Tool{Name: "search", Title: "Native Search", Description: "native description", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &sdk.ToolAnnotations{Title: "annotation title", ReadOnlyHint: true}})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(_ *http.Request, workspace string) bool { return workspace == "research" },
		Catalog: func(context.Context, string) ([]CatalogTool, error) {
			return []CatalogTool{{Name: "docs__search", RemoteName: "search", Declaration: declaration, NativeDescriptor: native}}, nil
		},
		Call: func(_ context.Context, workspace string, tool CatalogTool, args json.RawMessage) (*sdk.CallToolResult, error) {
			called = workspace == "research" && tool.Name == "docs__search" && string(args) == `{"q":"evidence"}`
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "found"}}}, nil
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: server.URL + "/workspaces/research/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "docs__search" {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	var expected sdk.Tool
	if err := json.Unmarshal(native, &expected); err != nil {
		t.Fatal(err)
	}
	expected.Name = "docs__search"
	if !reflect.DeepEqual(listed.Tools[0], &expected) {
		t.Fatalf("native descriptor changed: %#v", listed.Tools[0])
	}
	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "docs__search", Arguments: json.RawMessage(`{"q":"evidence"}`)})
	if err != nil || len(result.Content) != 1 || !called {
		t.Fatalf("result=%#v called=%t err=%v", result, called, err)
	}
}

func TestWorkspaceMCPHandlerRejectsManagementAndAmbiguousPaths(t *testing.T) {
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{Authorize: func(*http.Request, string) bool { return true }, Catalog: func(context.Context, string) ([]CatalogTool, error) { return nil, nil }, Call: func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error) {
		return nil, nil
	}})
	for _, path := range []string{"/api/v1/workspaces/research", "/workspaces/research/%6dcp", "/workspaces/research/mcp/extra", "/workspaces/research/mcp/../api/v1"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code == http.StatusOK {
			t.Fatalf("path %q reached MCP handler", path)
		}
	}
}

func TestWorkspaceMCPHandlerRejectsWrongMethodAndHostileOrigin(t *testing.T) {
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{Authorize: func(*http.Request, string) bool { return true }, Catalog: func(context.Context, string) ([]CatalogTool, error) { return nil, nil }, Call: func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error) {
		return nil, nil
	}})
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/workspaces/research/mcp", nil))
	if get.Code == http.StatusOK {
		t.Fatal("GET reached MCP transport")
	}
	origin := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/research/mcp", nil)
	req.Host = "node.example"
	req.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(origin, req)
	if origin.Code != http.StatusForbidden {
		t.Fatalf("origin status=%d", origin.Code)
	}
}

func TestWorkspaceMCPOriginRequiresExactSchemeAndAuthorityBeforeCatalog(t *testing.T) {
	for _, origin := range []string{"null", "https://node.example.evil.test", "https://evil.test/node.example", "https://node.example@evil.test", "https://node.example", "http://node.example/path", "http://node.example?redirect=evil", "http://node.example#fragment", "http://node.example:81", "://node.example"} {
		t.Run(origin, func(t *testing.T) {
			catalogCalled := false
			handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
				Authorize: func(*http.Request, string) bool { return true },
				Catalog:   func(context.Context, string) ([]CatalogTool, error) { catalogCalled = true; return nil, nil },
				Call: func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error) {
					return nil, nil
				},
			})
			request := httptest.NewRequest(http.MethodPost, "http://node.example/workspaces/research/mcp", nil)
			request.Header.Set("Origin", origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden || catalogCalled {
				t.Fatalf("hostile Origin reached catalog: status=%d called=%v", response.Code, catalogCalled)
			}
		})
	}
	for _, origin := range []string{"", "http://node.example"} {
		request := httptest.NewRequest(http.MethodPost, "http://node.example/workspaces/research/mcp", nil)
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{Authorize: func(*http.Request, string) bool { return true }})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("valid Origin rejected: %q status=%d", origin, response.Code)
		}
	}
}

func TestWorkspaceMCPOriginIgnoresForwardingAndRejectsRepeatedHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://node.example/workspaces/research/mcp", nil)
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Origin", "https://node.example")
	if !workspaceMCPOriginAllowed(request) {
		t.Fatal("exact TLS Origin rejected")
	}
	request.Header.Add("Origin", "https://evil.example")
	if workspaceMCPOriginAllowed(request) {
		t.Fatal("repeated Origin accepted")
	}
	request.Header.Set("Origin", "http://node.example")
	request.Header.Set("X-Forwarded-Proto", "http")
	if workspaceMCPOriginAllowed(request) {
		t.Fatal("forwarded scheme authorized Origin")
	}
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("X-Forwarded-Host", "evil.example")
	if workspaceMCPOriginAllowed(request) {
		t.Fatal("forwarded host authorized Origin")
	}
	request.Header.Set("Origin", "")
	if workspaceMCPOriginAllowed(request) {
		t.Fatal("explicit empty Origin accepted")
	}
}

func TestWorkspaceMCPHandlerRejectsMismatchedStandardHeaders(t *testing.T) {
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(object)
	key, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	declaration, _ := canonical.NewFunctionTool(key, "search", schema, canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(_ *http.Request, workspace string) bool { return workspace == "research" },
		Catalog: func(context.Context, string) ([]CatalogTool, error) {
			return []CatalogTool{{Name: "docs__search", RemoteName: "search", Declaration: declaration}}, nil
		},
		Call: func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{}, nil
		},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/workspaces/research/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	request.Header.Set("Mcp-Method", "tools/call")
	request.Header.Set("Mcp-Name", "docs__search")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q, want 400 header/body mismatch", recorder.Code, recorder.Body.String())
	}
}

func TestWorkspaceMCPHandlerServesLatestStatelessProtocol(t *testing.T) {
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	declarationKey, _ := canonical.NewToolKey("docs", canonical.ToolKindFunction, "search")
	declaration, _ := canonical.NewFunctionTool(declarationKey, "search", canonical.NewToolSchemaObject(object), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	called := false
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(*http.Request, string) bool { return true },
		Catalog: func(context.Context, string) ([]CatalogTool, error) {
			return []CatalogTool{{Name: "docs__search", RemoteName: "search", Declaration: declaration}}, nil
		},
		Call: func(_ context.Context, _ string, tool CatalogTool, _ json.RawMessage) (*sdk.CallToolResult, error) {
			called = tool.Name == "docs__search"
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "found"}}}, nil
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "latest-client", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: server.URL + "/workspaces/research/mcp", DisableStandaloneSSE: true, MaxRetries: -1}, &sdk.ClientSessionOptions{ProtocolVersion: "2026-07-28"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.InitializeResult().ProtocolVersion != "2026-07-28" {
		t.Fatalf("negotiated protocol=%q", session.InitializeResult().ProtocolVersion)
	}
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "docs__search" {
		t.Fatalf("catalog=%#v err=%v", listed, err)
	}
	result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{}})
	if err != nil || !called || result == nil || len(result.Content) != 1 || result.Content[0].(*sdk.TextContent).Text != "found" {
		t.Fatalf("call result=%#v called=%v err=%v", result, called, err)
	}
}

func TestWorkspaceMCPPathAcceptsOnlyCanonicalRoot(t *testing.T) {
	for _, tc := range []struct {
		path, slug string
		ok         bool
	}{{"/workspaces/research/mcp", "research", true}, {"/workspaces/research/mcp/", "", false}, {"/workspaces/research/v1/mcp", "", false}} {
		got, ok := workspaceMCPPath(tc.path)
		if got != tc.slug || ok != tc.ok {
			t.Fatalf("workspaceMCPPath(%q)=(%q,%t), want (%q,%t)", tc.path, got, ok, tc.slug, tc.ok)
		}
	}
}
