package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestPublicMCPAddressPolicy(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "::1", "fe80::1", "2001:db8::1"} {
		if publicMCPAddress(netip.MustParseAddr(raw)) {
			t.Fatalf("address %s was admitted", raw)
		}
	}
	if !publicMCPAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address was rejected")
	}
	if publicMCPAddress(netip.MustParseAddr("::ffff:127.0.0.1")) {
		t.Fatal("IPv4-mapped loopback address was admitted")
	}
	if publicMCPResolution([]netip.Addr{
		netip.MustParseAddr("8.8.8.8"),
		netip.MustParseAddr("10.0.0.1"),
	}) {
		t.Fatal("mixed public/private DNS resolution was admitted")
	}
}

func TestToolListBoundsRejectCyclesAndLimits(t *testing.T) {
	cycle := toolListBounds{seenCursors: map[string]struct{}{}}
	if err := cycle.observe(1, "next"); err != nil {
		t.Fatal(err)
	}
	if err := cycle.observe(1, "next"); err == nil {
		t.Fatal("repeated cursor was admitted")
	}
	tooMany := toolListBounds{seenCursors: map[string]struct{}{}}
	if err := tooMany.observe(MaxToolsPerSource+1, ""); err == nil {
		t.Fatal("oversized catalog was admitted")
	}
	tooManyPages := toolListBounds{seenCursors: map[string]struct{}{}}
	for page := 0; page < MaxListPages; page++ {
		if err := tooManyPages.observe(0, string(rune(page+1))); err != nil {
			t.Fatal(err)
		}
	}
	if err := tooManyPages.observe(0, "overflow"); err == nil {
		t.Fatal("page limit was not enforced")
	}
	bytes := toolListBounds{seenCursors: map[string]struct{}{}}
	if err := bytes.observeBytes(MaxCatalogBytesPerSource + 1); err == nil {
		t.Fatal("oversized aggregate catalog was admitted")
	}
}

func TestSDKCatalogBytesIncludeDroppedOutputAndExpandedDescription(t *testing.T) {
	tool := &mcp.Tool{
		Name:         "search",
		Description:  "tool description",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["answer"]}`),
	}
	got, err := sdkToolRetainedBytes("source description", tool)
	if err != nil {
		t.Fatal(err)
	}
	minimum := len(tool.Name) + len(tool.Description) +
		len(tool.InputSchema.(json.RawMessage)) +
		len(tool.OutputSchema.(json.RawMessage)) +
		len("source description\n\ntool description")
	if got < minimum {
		t.Fatalf("catalog bytes = %d, want at least %d", got, minimum)
	}
}

func TestMCPResponseAndSchemaLimits(t *testing.T) {
	body := &maxBytesReadCloser{
		source:    io.NopCloser(strings.NewReader(strings.Repeat("x", MaxHTTPResponseBytes+1))),
		remaining: MaxHTTPResponseBytes,
	}
	if _, err := io.ReadAll(body); err == nil {
		t.Fatal("oversized HTTP response was admitted")
	}
	if _, err := schemaFromSDK(map[string]any{"description": strings.Repeat("x", MaxSchemaBytes+1)}); err == nil {
		t.Fatal("oversized schema was admitted")
	}
}

func TestBearerTransportConfinesAuthorizationToExactOrigin(t *testing.T) {
	var headers []string
	next := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		headers = append(headers, request.Header.Get("Authorization"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("{}")),
			Header:     make(http.Header),
		}, nil
	})
	transport := bearerTransport{
		next: next, origin: "https://mcp.example.test", bearer: "secret",
	}
	for _, raw := range []string{
		"https://mcp.example.test/rpc",
		"https://other.example.test/rpc",
	} {
		endpoint, _ := url.Parse(raw)
		request := &http.Request{Method: http.MethodPost, URL: endpoint, Header: make(http.Header)}
		if _, err := transport.RoundTrip(request); err != nil {
			t.Fatal(err)
		}
	}
	if len(headers) != 2 || headers[0] != "Bearer secret" || headers[1] != "" {
		t.Fatalf("authorization headers = %#v", headers)
	}
}

func TestMCPToolOutputSchemaIsApproximationEvidence(t *testing.T) {
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	remote, _ := canonical.NewMCPSource("https://mcp.example.test/rpc", canonical.Unspecified[[]string]())
	sourceDeclaration, _ := canonical.NewMCPToolNamespace(sourceKey, "", remote, nil)
	source, _ := sourceDeclaration.Namespace()
	declaration, decisions, err := declarationFromSDKTool(source, &mcp.Tool{
		Name: "structured", InputSchema: map[string]any{"type": "object"},
		OutputSchema: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := declaration.Function(); !ok || len(decisions) != 1 {
		t.Fatalf("output-schema approximation = %#v / %#v", declaration, decisions)
	}
}

func TestMCPSemanticToolLoopUsesOfficialSDKSession(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{
		Name: "search", Description: "Search",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
	}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var arguments map[string]any
		_ = json.Unmarshal(request.Params.Arguments, &arguments)
		if oversized, _ := arguments["oversized"].(bool); oversized {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("x", MaxToolResultBytes+1)}}}, nil
		}
		if structured, _ := arguments["structured"].(bool); structured {
			return &mcp.CallToolResult{StructuredContent: map[string]any{"answer": 1}}, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(request.Params.Arguments)}}}, nil
	})
	server.AddTool(&mcp.Tool{
		Name: "unsupported", Description: "Unsupported",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.ImageContent{
				Data: []byte("aW1hZ2U="), MIMEType: "image/png",
			}},
		}, nil
	})
	server.AddTool(&mcp.Tool{
		Name: "fetch", Description: "Fetch",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "fetched"}}}, nil
	})
	server.AddTool(&mcp.Tool{
		Name: "slow", Description: "Slow",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	sdkSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	remote, _ := canonical.NewMCPSource(
		"https://mcp.example.test/rpc", canonical.Specify([]string{"fetch", "search"}),
	)
	sourceDeclaration, _ := canonical.NewMCPToolNamespace(sourceKey, "", remote, nil)
	source, _ := sourceDeclaration.Namespace()
	activeSession := &session{source: source, sdk: sdkSession}
	defer activeSession.close()

	declarations, decisions, err := activeSession.listTools(ctx)
	if err != nil || len(declarations) != 2 {
		t.Fatalf("declarations = %#v, %v", declarations, err)
	}
	if len(decisions) != 1 || decisions[0].Outcome != compat.Approx {
		t.Fatalf("output-schema decisions = %#v", decisions)
	}
	first, _ := declarations[0].Function()
	if first.Key().Name() != "fetch" {
		t.Fatalf("allowed_tools order was lost: %#v", declarations)
	}
	tool, _ := declarations[1].Function()
	input, _ := canonical.ParseJSONObject([]byte(`{"q":"docs"}`))
	parts, isError, err := activeSession.call(ctx, tool.Key().Name(), input)
	if err != nil || isError || len(parts) != 1 {
		t.Fatalf("result = %#v isError=%v err=%v", parts, isError, err)
	}
	text, _ := parts[0].Text()
	if !strings.Contains(text.Text(), `"q":"docs"`) {
		t.Fatalf("result text = %q", text.Text())
	}

	run := &Run{
		sessions: map[canonical.ToolKey]*session{sourceKey: activeSession},
		bindings: map[canonical.ToolKey]binding{
			tool.Key(): {source: sourceKey, remoteName: tool.Key().Name()},
		},
	}
	callID, _ := canonical.NewToolCallID("call_search")
	callItem, err := canonical.NewToolCallItem(
		callID, tool.Key(), canonical.NewJSONObjectToolInput(input),
	)
	if err != nil {
		t.Fatal(err)
	}
	calls, err := run.Calls(runtimeTestResponse(t, callItem))
	if err != nil || len(calls) != 1 {
		t.Fatalf("provider call classification = %#v, %v", calls, err)
	}
	if err := run.BeginBatch(calls); err != nil {
		t.Fatal(err)
	}
	resultItem, err := run.Call(ctx, calls[0])
	if err != nil {
		t.Fatal(err)
	}
	result, ok := resultItem.ToolResult()
	if !ok || result.CallID() != callID {
		t.Fatalf("correlated MCP result = %#v", resultItem)
	}
	catalogDeclaration, err := canonical.NewMCPToolNamespace(
		sourceKey, "", remote, declarations,
	)
	if err != nil {
		t.Fatal(err)
	}
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{catalogDeclaration})
	if err != nil {
		t.Fatal(err)
	}
	declarationItem, err := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	userItem, err := canonical.NewMessageItem(
		canonical.MessageRoleUser,
		[]canonical.MessagePart{canonical.NewTextMessagePart("search docs")},
	)
	if err != nil {
		t.Fatal(err)
	}
	continuation := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			declarationItem, userItem, callItem, resultItem,
		},
	})
	if err := canonical.ValidateMaterializedRequest(continuation); err != nil {
		t.Fatalf("MCP continuation is invalid: %v", err)
	}
	finalItem, err := canonical.NewMessageItem(
		canonical.MessageRoleAssistant,
		[]canonical.MessagePart{canonical.NewTextMessagePart("done")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp_mcp_final"}, "model",
		[]canonical.CanonicalItem{finalItem}, canonical.Completed("stop"),
		canonical.NewUnknownTokenUsage(),
	); err != nil {
		t.Fatalf("MCP final answer is invalid: %v", err)
	}

	oversized, _ := canonical.ParseJSONObject([]byte(`{"oversized":true}`))
	if _, _, err := activeSession.call(ctx, tool.Key().Name(), oversized); err == nil {
		t.Fatal("oversized tool result was admitted")
	}
	structured, _ := canonical.ParseJSONObject([]byte(`{"structured":true}`))
	parts, isError, err = activeSession.call(ctx, tool.Key().Name(), structured)
	if err != nil || isError || len(parts) != 1 {
		t.Fatalf("structured result = %#v isError=%v err=%v", parts, isError, err)
	}
	structuredText, _ := parts[0].Text()
	if structuredText.Text() != `{"answer":1}` {
		t.Fatalf("structured result text = %q", structuredText.Text())
	}
	parts, isError, err = activeSession.call(ctx, "unsupported", input)
	if err != nil || !isError || len(parts) != 1 {
		t.Fatalf("unsupported result = %#v isError=%v err=%v", parts, isError, err)
	}
	unsupportedText, _ := parts[0].Text()
	if !strings.Contains(unsupportedText.Text(), "may have completed") {
		t.Fatalf("unsupported result text = %q", unsupportedText.Text())
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := activeSession.call(cancelled, "slow", input); err == nil {
		t.Fatal("cancelled MCP call did not terminate")
	}
	started := time.Now()
	if _, _, err := activeSession.callWithTimeout(ctx, "slow", input, 20*time.Millisecond); err == nil {
		t.Fatal("hanging MCP call outlived its adapter deadline")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("hanging MCP call terminated after %s", elapsed)
	}
}

func jsonObject(t *testing.T, raw string) canonical.JSONObject {
	t.Helper()
	object, err := canonical.ParseJSONObject([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return object
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
