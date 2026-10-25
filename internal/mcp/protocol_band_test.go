package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestWorkspaceMCPNegotiatesPinnedVersionsAndRejectsUnsupportedDispatch(t *testing.T) {
	key, err := canonical.NewToolKey("docs", canonical.ToolKindFunction, "fetch")
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := declarationFromSDKToolForNumericTest(key)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(*http.Request, string) bool { return true },
		Catalog: func(context.Context, string) ([]CatalogTool, error) {
			return []CatalogTool{{Name: "docs__fetch", RemoteName: "fetch", Declaration: declaration}}, nil
		},
		Call: func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error) {
			calls.Add(1)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "done"}}}, nil
		},
	})
	for _, version := range []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25", "2026-06-30", "2026-07-28", "invalid"} {
		t.Run(version, func(t *testing.T) {
			// 2026-07-28 negotiates through server/discover; this legacy
			// initialize matrix deliberately verifies only handshake-era versions.
			supported := version == "2024-11-05" || version == "2025-03-26" || version == "2025-06-18" || version == "2025-11-25"
			invoke := func(method, params, header string) *httptest.ResponseRecorder {
				t.Helper()
				request := httptest.NewRequest(http.MethodPost, "/workspaces/research/mcp", strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":%s}`, method, params)))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Accept", "application/json, text/event-stream")
				if header != "" {
					request.Header.Set("Mcp-Protocol-Version", header)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Mcp-Session-Id") != "" {
					t.Fatal("version probe changed caching or statelessness")
				}
				return response
			}
			initialized := invoke("initialize", fmt.Sprintf(`{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"version-probe","version":"1"}}`, version), "")
			negotiated := version
			if !supported {
				negotiated = "2025-11-25"
			}
			if initialized.Code != http.StatusOK || !strings.Contains(initialized.Body.String(), `"protocolVersion":"`+negotiated+`"`) {
				t.Fatalf("negotiation=%d %s", initialized.Code, initialized.Body.String())
			}
			listed := invoke("tools/list", `{}`, negotiated)
			if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"name":"docs__fetch"`) {
				t.Fatalf("negotiated list=%d %s", listed.Code, listed.Body.String())
			}
			before := calls.Load()
			called := invoke("tools/call", `{"name":"docs__fetch","arguments":{}}`, version)
			if supported {
				if called.Code != http.StatusOK || !strings.Contains(called.Body.String(), `"text":"done"`) || calls.Load() != before+1 {
					t.Fatalf("supported call=%d %s", called.Code, called.Body.String())
				}
			} else if called.Code != http.StatusBadRequest || calls.Load() != before {
				t.Fatalf("unsupported dispatch=%d effects=%d", called.Code, calls.Load()-before)
			}
		})
	}
}

func TestWorkspaceMCPAdvertisesAndServes20260728Discovery(t *testing.T) {
	key, err := canonical.NewToolKey("docs", canonical.ToolKindFunction, "fetch")
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := declarationFromSDKToolForNumericTest(key)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	handler := NewWorkspaceMCPHandler(WorkspaceServerConfig{
		Authorize: func(*http.Request, string) bool { return true },
		Catalog: func(context.Context, string) ([]CatalogTool, error) {
			return []CatalogTool{{Name: "docs__fetch", RemoteName: "fetch", Declaration: declaration}}, nil
		},
		Call: func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error) {
			calls.Add(1)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "current protocol"}}}, nil
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	requestID, err := jsonrpc.MakeID("1")
	if err != nil {
		t.Fatal(err)
	}
	discover := &jsonrpc.Request{Method: "server/discover", ID: requestID, Params: mustJSON(t, &sdk.DiscoverParams{
		Meta: sdk.Meta{
			sdk.MetaKeyProtocolVersion:    "2026-07-28",
			sdk.MetaKeyClientInfo:         &sdk.Implementation{Name: "band-probe", Version: "1"},
			sdk.MetaKeyClientCapabilities: &sdk.ClientCapabilities{},
		},
	})}
	payload, err := jsonrpc.EncodeMessage(discover)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/workspaces/research/mcp", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	request.Header.Set("Mcp-Method", "server/discover")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"2026-07-28"`) || response.Header.Get("Mcp-Session-Id") != "" {
		t.Fatalf("server/discover status=%d body=%s headers=%v", response.StatusCode, body, response.Header)
	}
	if calls.Load() != 0 {
		t.Fatalf("protocol discovery executed a source tool: calls=%d", calls.Load())
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestProductionMCPSessionRejectsPrivateDestinationBeforeSourceEffect(t *testing.T) {
	var effects atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { effects.Add(1) }))
	defer server.Close()
	for _, endpoint := range []string{server.URL, strings.Replace(server.URL, "https://", "http://", 1)} {
		remote, err := newTestMCPURL(endpoint, canonical.Unspecified[[]string]())
		if err != nil {
			t.Fatal(err)
		}
		key, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "source")
		if err != nil {
			t.Fatal(err)
		}
		declaration, err := canonical.NewMCPToolSource(key, "", remote, nil)
		if err != nil {
			t.Fatal(err)
		}
		source, _ := declaration.MCP()
		session, err := openSession(t.Context(), source, SourceAccess{Authorization: "synthetic-not-a-secret"})
		if err == nil {
			_ = session.close()
			t.Fatal("private/insecure MCP source admitted")
		}
		if strings.Contains(err.Error(), "synthetic-not-a-secret") {
			t.Fatal("source denial exposed authorization")
		}
	}
	if effects.Load() != 0 {
		t.Fatal("denied source received a request")
	}
}

func TestNativeCapturePreservesProductionCredentialOriginConfinement(t *testing.T) {
	remote, err := newTestMCPURL("https://source.example/mcp", canonical.Unspecified[[]string]())
	if err != nil {
		t.Fatal(err)
	}
	key, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "source")
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := canonical.NewMCPToolSource(key, "", remote, nil)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := declaration.MCP()
	client, _, err := safeHTTPClient(source, SourceAccess{Authorization: "synthetic-not-a-secret", Headers: map[string]string{"X-Source-Access": "synthetic-header"}})
	if err != nil {
		t.Fatal(err)
	}
	status := client.Transport.(authenticationStatusTransport)
	bearer := status.next.(bearerTransport)
	headers := bearer.next.(headerTransport)
	limits := headers.next.(responseLimitTransport)
	limits.next = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "source.example" {
			if request.Header.Get("Authorization") != "Bearer synthetic-not-a-secret" || request.Header.Get("X-Source-Access") != "synthetic-header" {
				t.Error("capture removed source authority")
			}
		} else if request.Header.Get("Authorization") != "" || request.Header.Get("X-Source-Access") != "" {
			t.Error("capture leaked authority across origins")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{"value":9007199254740993}}`))}, nil
	})
	headers.next = limits
	bearer.next = headers
	status.next = bearer
	client.Transport = nativeResponseTransport{next: status}
	for _, endpoint := range []string{"https://source.example/mcp", "https://other.example/mcp"} {
		capture := &nativeResponseCapture{}
		request, err := http.NewRequestWithContext(context.WithValue(t.Context(), nativeResponseKey{}, capture), http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := capture.result(t.Context())
		if err != nil || string(raw) != `{"value":9007199254740993}` {
			t.Fatalf("capture changed credentialed response: %s %v", raw, err)
		}
	}
}
