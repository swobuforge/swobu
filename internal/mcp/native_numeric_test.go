package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type numericProbeTransport struct{ sdk.Transport }

type numericProbeConnection struct{ sdk.Connection }

func (transport numericProbeTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	connection, err := transport.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return numericProbeConnection{Connection: connection}, nil
}

func TestSDKConnectionWrapperLosesNegotiatedProtocolHeader(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		server := sdk.NewServer(&sdk.Implementation{Name: "source", Version: "1"}, &sdk.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}})
		server.AddTool(&sdk.Tool{Name: "fetch", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{}, nil
		})
		var observed string
		delegate := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true})
		httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
			}
			request.Body = io.NopCloser(bytes.NewReader(body))
			if bytes.Contains(body, []byte(`"method":"tools/list"`)) {
				observed = request.Header.Get("Mcp-Protocol-Version")
			}
			delegate.ServeHTTP(writer, request)
		}))
		var transport sdk.Transport = &sdk.StreamableClientTransport{Endpoint: httpServer.URL, DisableStandaloneSSE: true, MaxRetries: -1}
		if wrapped {
			transport = numericProbeTransport{Transport: transport}
		}
		client := sdk.NewClient(&sdk.Implementation{Name: "probe", Version: "1"}, nil)
		session, err := client.Connect(t.Context(), transport, &sdk.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
		if err != nil {
			httpServer.Close()
			t.Fatal(err)
		}
		_, err = session.ListTools(t.Context(), nil)
		_ = session.Close()
		httpServer.Close()
		if err != nil {
			t.Fatal(err)
		}
		if wrapped && observed != "" {
			t.Fatalf("SDK wrapper seam changed; reassess native decode design: %q", observed)
		}
		if !wrapped && observed == "" {
			t.Fatal("unwrapped SDK failed to propagate negotiated protocol")
		}
	}
}

func TestPinnedSDKNativeDescriptorAndResultRoundSourceNumbers(t *testing.T) {
	ctx := t.Context()
	server := sdk.NewServer(&sdk.Implementation{Name: "numeric-source", Version: "1"}, nil)
	numbers := json.RawMessage(`{"integer":9007199254740993,"decimal":0.12345678901234567890123456789}`)
	tool := &sdk.Tool{Name: "fetch", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer","minimum":9007199254740993}}}`), Meta: sdk.Meta{"source/numbers": numbers}}
	server.AddTool(tool, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "done", Meta: sdk.Meta{"source/numbers": numbers}}}, StructuredContent: numbers, Meta: sdk.Meta{"source/numbers": numbers}}, nil
	})
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
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
	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 1 {
		t.Fatalf("list: %v %#v", err, listed)
	}
	result, err := clientSession.CallTool(ctx, &sdk.CallToolParams{Name: "fetch", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"descriptor": listed.Tools[0], "structured": result.StructuredContent, "result metadata": result.Meta, "content metadata": result.Content[0]} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), "9007199254740992") || !strings.Contains(string(encoded), "0.12345678901234568") {
			t.Errorf("%s changed pinned SDK numeric behavior; reassess native fidelity remedy: %s", name, encoded)
		}
	}
}

func TestOuterNumberDecoderCannotRepairSDKNativeResultUnmarshal(t *testing.T) {
	for _, item := range []struct {
		name      string
		raw       string
		value     any
		preserved bool
	}{
		{"descriptor", `{"name":"fetch","inputSchema":{"type":"object","minimum":9007199254740993},"_meta":{"number":9007199254740993}}`, &sdk.Tool{}, true},
		{"result", `{"content":[{"type":"text","text":"done","_meta":{"number":9007199254740993}}],"structuredContent":{"number":9007199254740993},"_meta":{"number":9007199254740993}}`, &sdk.CallToolResult{}, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			decoder := json.NewDecoder(bytes.NewReader([]byte(item.raw)))
			decoder.UseNumber()
			if err := decoder.Decode(item.value); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(item.value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "9007199254740993") != item.preserved {
				t.Fatalf("SDK decoder boundary changed; reassess remedy: %s", encoded)
			}
		})
	}
}

func TestHTTPNativeResponsePreservesNumbersAndNegotiatedProtocol(t *testing.T) {
	for _, jsonResponse := range []bool{false, true} {
		server := sdk.NewServer(&sdk.Implementation{Name: "numeric-source", Version: "1"}, &sdk.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}})
		numbers := json.RawMessage(`{"integer":9007199254740993,"decimal":0.12345678901234567890123456789}`)
		server.AddTool(&sdk.Tool{Name: "fetch", InputSchema: json.RawMessage(`{"type":"object","minimum":9007199254740993}`), Meta: sdk.Meta{"source/numbers": numbers}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{StructuredContent: numbers, Meta: sdk.Meta{"source/numbers": numbers}, Content: []sdk.Content{
				&sdk.TextContent{Text: "done", Meta: sdk.Meta{"source/numbers": numbers}},
				&sdk.ImageContent{Data: []byte{1}, MIMEType: "image/png", Meta: sdk.Meta{"source/numbers": numbers}},
				&sdk.AudioContent{Data: []byte{2}, MIMEType: "audio/wav", Meta: sdk.Meta{"source/numbers": numbers}},
				&sdk.ResourceLink{URI: "source://link", Name: "link", Meta: sdk.Meta{"source/numbers": numbers}},
				&sdk.EmbeddedResource{Resource: &sdk.ResourceContents{URI: "source://value", Text: "done", Meta: sdk.Meta{"source/numbers": numbers}}, Meta: sdk.Meta{"source/numbers": numbers}},
			}}, nil
		})
		delegate := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: jsonResponse})
		var protocol string
		httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
			}
			request.Body = io.NopCloser(bytes.NewReader(body))
			if bytes.Contains(body, []byte(`"method":"tools/list"`)) {
				protocol = request.Header.Get("Mcp-Protocol-Version")
			}
			delegate.ServeHTTP(writer, request)
		}))
		client := sdk.NewClient(&sdk.Implementation{Name: "numeric-client", Version: "1"}, nil)
		client.AddSendingMiddleware(nativeResponseMiddleware)
		transport := &sdk.StreamableClientTransport{Endpoint: httpServer.URL, DisableStandaloneSSE: true, MaxRetries: -1, HTTPClient: &http.Client{Transport: nativeResponseTransport{next: responseLimitTransport{next: http.DefaultTransport}}}}
		session, err := client.Connect(t.Context(), transport, &sdk.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
		if err != nil {
			httpServer.Close()
			t.Fatal(err)
		}
		listed, err := session.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "fetch", Arguments: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		_ = session.Close()
		httpServer.Close()
		if protocol == "" {
			t.Fatal("native response capture lost negotiated protocol")
		}
		native, err := freezeNativeTool(listed.Tools[0])
		if err != nil {
			t.Fatal(err)
		}
		namespace, _ := canonical.NewToolKey("docs", canonical.ToolKindFunction, "fetch")
		declaration, err := declarationFromSDKToolForNumericTest(namespace)
		if err != nil {
			t.Fatal(err)
		}
		workspaceServer := httptest.NewServer(NewWorkspaceMCPHandler(WorkspaceServerConfig{
			Authorize: func(*http.Request, string) bool { return true },
			Catalog: func(context.Context, string) ([]CatalogTool, error) {
				return []CatalogTool{{Name: "docs__fetch", RemoteName: "fetch", NativeDescriptor: native, Declaration: declaration}}, nil
			},
			Call: func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error) {
				return result, nil
			},
		}))
		for _, method := range []string{"tools/list", "tools/call"} {
			params := `{}`
			if method == "tools/call" {
				params = `{"name":"docs__fetch","arguments":{}}`
			}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, workspaceServer.URL+"/workspaces/research/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":99,"method":"`+method+`","params":`+params+`}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			request.Header.Set("Mcp-Protocol-Version", protocol)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || response.StatusCode != http.StatusOK || !bytes.Contains(wire, []byte("9007199254740993")) || !bytes.Contains(wire, []byte("0.12345678901234567890123456789")) || bytes.Contains(wire, []byte("9007199254740992")) {
				t.Fatalf("Workspace %s changed source numbers: status=%d err=%v body=%s", method, response.StatusCode, err, wire)
			}
		}
		workspaceServer.Close()
		for name, value := range map[string]any{"descriptor": listed.Tools[0], "result": result} {
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(encoded), "9007199254740993") || !strings.Contains(string(encoded), "0.12345678901234567890123456789") || strings.Contains(string(encoded), "9007199254740992") {
				t.Fatalf("%s lost source numeric truth: %s", name, encoded)
			}
		}
		for index, content := range result.Content {
			encoded, err := json.Marshal(content)
			if err != nil || !bytes.Contains(encoded, []byte("9007199254740993")) || !bytes.Contains(encoded, []byte("0.12345678901234567890123456789")) {
				t.Fatalf("content %d lost numeric metadata: %s %v", index, encoded, err)
			}
		}
	}
}

func TestNativeHTTPParallelCallsKeepExactResponsesIsolated(t *testing.T) {
	for _, jsonResponse := range []bool{false, true} {
		server := sdk.NewServer(&sdk.Implementation{Name: "parallel", Version: "1"}, nil)
		server.AddTool(&sdk.Tool{Name: "fetch", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(_ context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "done"}}, StructuredContent: json.RawMessage(request.Params.Arguments)}, nil
		})
		httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: jsonResponse}))
		client := sdk.NewClient(&sdk.Implementation{Name: "parallel-client", Version: "1"}, nil)
		client.AddSendingMiddleware(nativeResponseMiddleware)
		session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: httpServer.URL, DisableStandaloneSSE: true, MaxRetries: -1, HTTPClient: &http.Client{Transport: nativeResponseTransport{next: responseLimitTransport{next: http.DefaultTransport}}}}, &sdk.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
		if err != nil {
			httpServer.Close()
			t.Fatal(err)
		}
		var workers sync.WaitGroup
		for index := 0; index < 24; index++ {
			workers.Add(1)
			go func(index int) {
				defer workers.Done()
				args := json.RawMessage(fmt.Sprintf(`{"integer":%d,"decimal":0.12345678901234567890123456789}`, int64(9007199254740993)+int64(index)*2))
				result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "fetch", Arguments: args})
				if err != nil {
					t.Error(err)
					return
				}
				encoded, err := json.Marshal(result.StructuredContent)
				var expected any
				decoder := json.NewDecoder(bytes.NewReader(args))
				decoder.UseNumber()
				if decodeErr := decoder.Decode(&expected); decodeErr != nil {
					t.Error(decodeErr)
					return
				}
				want, _ := json.Marshal(expected)
				if err != nil || !bytes.Equal(encoded, want) {
					t.Errorf("parallel response changed identity or number: %s want %s err=%v", encoded, want, err)
				}
			}(index)
		}
		workers.Wait()
		_ = session.Close()
		httpServer.Close()
	}
}

func TestNativeHTTPCatalogAcceptsSSEFrameWithinSourceByteBudget(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "large-catalog", Version: "1"}, nil)
	server.AddTool(&sdk.Tool{Name: "fetch", InputSchema: json.RawMessage(`{"type":"object"}`), Meta: sdk.Meta{"large": strings.Repeat("x", (1<<20)+1024)}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{}, nil
	})
	httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "large-client", Version: "1"}, nil)
	client.AddSendingMiddleware(nativeResponseMiddleware)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: httpServer.URL, DisableStandaloneSSE: true, MaxRetries: -1, HTTPClient: &http.Client{Transport: nativeResponseTransport{next: responseLimitTransport{next: http.DefaultTransport}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil || len(listed.Tools) != 1 {
		t.Fatalf("valid bounded catalog rejected: %v", err)
	}
}

func TestNativeHTTPCancellationDoesNotRetryOrContaminateLaterResponse(t *testing.T) {
	for _, jsonResponse := range []bool{false, true} {
		server := sdk.NewServer(&sdk.Implementation{Name: "cancel-source", Version: "1"}, nil)
		started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		server.AddTool(&sdk.Tool{Name: "fetch", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-release
				defer close(finished)
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "late"}}, StructuredContent: json.RawMessage(`{"integer":9007199254740993}`)}, nil
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "fresh"}}, StructuredContent: json.RawMessage(`{"integer":9007199254740995}`)}, nil
		})
		httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: jsonResponse}))
		client := sdk.NewClient(&sdk.Implementation{Name: "cancel-client", Version: "1"}, nil)
		client.AddSendingMiddleware(nativeResponseMiddleware)
		session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: httpServer.URL, DisableStandaloneSSE: true, MaxRetries: -1, HTTPClient: &http.Client{Transport: nativeResponseTransport{next: responseLimitTransport{next: http.DefaultTransport}}}}, nil)
		if err != nil {
			httpServer.Close()
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		outcome := make(chan error, 1)
		go func() {
			_, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "fetch", Arguments: map[string]any{}})
			outcome <- err
		}()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("source did not start")
		}
		cancel()
		select {
		case err := <-outcome:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("cancellation truth lost: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("cancelled call did not return")
		}
		close(release)
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("source did not finish")
		}
		result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "fetch", Arguments: map[string]any{}})
		if err != nil {
			t.Error(err)
		} else {
			encoded, err := json.Marshal(result.StructuredContent)
			if err != nil || string(encoded) != `{"integer":9007199254740995}` {
				t.Errorf("late response contaminated new call: %s %v", encoded, err)
			}
		}
		if calls.Load() != 2 {
			t.Errorf("cancelled effect retried: calls=%d", calls.Load())
		}
		_ = session.Close()
		httpServer.Close()
	}
}

func declarationFromSDKToolForNumericTest(key canonical.ToolKey) (canonical.ToolDeclaration, error) {
	schema, err := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	if err != nil {
		return canonical.ToolDeclaration{}, err
	}
	return canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(schema), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
}
