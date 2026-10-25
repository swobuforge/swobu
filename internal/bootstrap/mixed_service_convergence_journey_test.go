package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// The three Workspace Services must be independently executable and admitted
// while sharing the same Node snapshot and Principal universe. A passing suite
// of separate model/MCP/Search tests cannot establish this invariant.
//
// Models and Search have real loopback HTTP upstreams; MCP uses a revision-bound
// fixture source but is accessed through the real, official SDK and Node mux.
// This is not evidence of a third-party provider or remote TLS deployment.
func TestMixedServiceWorkspaceExecutionAuthorityAndRestart(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	root := t.TempDir()
	t.Setenv(config.EnvSwobuHome, root)
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	var modelCalls, searchCalls, toolCalls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			t.Errorf("unexpected Models upstream request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "wrong path", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, workspaceToolRoundResponse("responses", "", true))
	}))
	defer modelServer.Close()
	searchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"url":"https://example.test/answer","title":"Answer"}]}`)
	}))
	defer searchServer.Close()

	writeCurrentInferenceFixture(t, path, root, modelServer.URL, "responses", false)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture intentionally starts with an already-configured tri-Service
	// Workspace; activation-review mutations are separately qualified in S2.
	mixed := strings.Replace(string(raw), "  research:\n", "  research:\n    enabled_services: [models, mcp, search]\n    search:\n      connection_id: cn_mix_search\n    mcp_mounts:\n      docs:\n        connection_id: cn_mix_mcp\n        allowed_tools: [search]\n", 1)
	if mixed == string(raw) {
		t.Fatal("fixture Workspace missing")
	}
	if err := os.WriteFile(path, []byte(mixed), 0o600); err != nil {
		t.Fatal(err)
	}
	publishMCPFixtureConnection(t, path, "cn_mix_mcp")
	searchID, err := connectionidentity.Parse("cn_mix_search")
	if err != nil {
		t.Fatal(err)
	}
	searchEndpoint, err := connections.ParseEndpoint(searchServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	searchSpec, err := connections.NewSearchSpec("fixture", searchEndpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	searchConnection, err := connections.New(searchID, "Search loopback", searchSpec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	publishCurrentConnectionFixtures(t, path, searchConnection)

	schema, err := canonical.ParseJSONObject([]byte(`{"type":"object","properties":{"query":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	toolKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	tool, err := canonical.NewFunctionTool(toolKey, "Search", canonical.NewToolSchemaObject(schema), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	if err != nil {
		t.Fatal(err)
	}
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	declaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{tool})
	if err != nil {
		t.Fatal(err)
	}
	source, _ := declaration.MCP()
	input := StartInput{
		ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
		MCPConnectionBindings: map[string]mcp.SourceBinding{
			"cn_mix_mcp": {
				ID: "cn_mix_mcp", ConnectionID: "cn_mix_mcp", ConnectionRevision: "1", Source: source,
				Call: func(_ context.Context, name string, args json.RawMessage) (*sdk.CallToolResult, error) {
					toolCalls.Add(1)
					if name != "search" || string(args) != `{"query":"mixed"}` {
						return nil, os.ErrInvalid
					}
					return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "mixed MCP result"}}}, nil
				},
			},
		},
	}
	daemon, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = daemon.Close() }()
	workspace, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	all, allCredential, err := daemon.accessStore.Create("all-services", workspace)
	if err != nil {
		t.Fatal(err)
	}
	modelsOnly, modelCredential, err := daemon.accessStore.Create("model-only", workspace)
	if err != nil {
		t.Fatal(err)
	}
	searchOnly, searchCredential, err := daemon.accessStore.Create("search-only", workspace)
	if err != nil {
		t.Fatal(err)
	}
	setServices := func(id accessidentity.ID, key string, services []routing.ServiceKind) {
		t.Helper()
		revision, err := daemon.accessStore.PermissionRevision(id, workspace)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := daemon.accessStore.ApplyPermissionServicesWithRatesChecked(t.Context(), id, workspace, revision, key, func() error { return nil }, services, nil); err != nil {
			t.Fatal(err)
		}
	}
	setServices(modelsOnly.ID, "models-only", []routing.ServiceKind{routing.ServiceModels})
	setServices(searchOnly.ID, "search-only", []routing.ServiceKind{routing.ServiceSearch})
	// Creation's default Service set must be checked explicitly; no assumption
	// about new grants is allowed to become an acceptance-test fixture.
	setServices(all.ID, "all-three", []routing.ServiceKind{routing.ServiceModels, routing.ServiceMCP, routing.ServiceSearch})

	request := func(method, suffix, token, body string, want int) string {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, daemon.BaseURL()+suffix, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, suffix, response.StatusCode, want, raw)
		}
		return string(raw)
	}
	models := "/workspaces/research/v1/responses"
	searchPath := "/workspaces/research/search?query=mixed&limit=1"
	modelBody := `{"model":"chat","input":"hello"}`
	if out := request(http.MethodPost, models, modelCredential.Token, modelBody, 200); !strings.Contains(out, "finished") {
		t.Fatalf("Models result missing: %s", out)
	}
	if out := request(http.MethodGet, searchPath, searchCredential.Token, "", 200); !strings.Contains(out, "example.test/answer") {
		t.Fatalf("Search result missing: %s", out)
	}
	request(http.MethodGet, searchPath, modelCredential.Token, "", 403)
	request(http.MethodPost, models, searchCredential.Token, modelBody, 403)
	// Credential-free loopback is the native operator authority. A presented
	// invalid credential must fail instead of falling back to that authority.
	request(http.MethodPost, models, "invalid-workspace-credential", modelBody, 401)
	if modelCalls.Load() != 1 || searchCalls.Load() != 1 {
		t.Fatalf("denied Service dispatched upstream: models=%d search=%d", modelCalls.Load(), searchCalls.Load())
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "mixed-services-convergence", Version: "1"}, nil)
	connectMCP := func(token string) (*sdk.ClientSession, error) {
		return client.Connect(t.Context(), &sdk.StreamableClientTransport{
			Endpoint:             daemon.BaseURL() + "/workspaces/research/mcp",
			HTTPClient:           &http.Client{Transport: &bearerRoundTripper{next: http.DefaultTransport, token: token}},
			DisableStandaloneSSE: true,
		}, nil)
	}
	if denied, err := connectMCP(modelCredential.Token); err == nil {
		_ = denied.Close()
		t.Fatal("Models-only Principal entered MCP")
	}
	if denied, err := connectMCP(searchCredential.Token); err == nil {
		_ = denied.Close()
		t.Fatal("Search-only Principal entered MCP")
	}
	// Assert HTTP admission as well as SDK refusal: handshake failure alone
	// cannot distinguish permission denial from a broken transport.
	for _, token := range []string{modelCredential.Token, searchCredential.Token} {
		request(http.MethodPost, "/workspaces/research/mcp", token, `{}`, 403)
	}
	session, err := connectMCP(allCredential.Token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil || listed == nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "docs__search" {
		t.Fatalf("MCP catalog=%#v err=%v", listed, err)
	}
	result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{"query": "mixed"}})
	if err != nil || result == nil || len(result.Content) != 1 {
		t.Fatalf("MCP call=%#v err=%v", result, err)
	}
	if text, ok := result.Content[0].(*sdk.TextContent); !ok || text.Text != "mixed MCP result" || toolCalls.Load() != 1 {
		t.Fatalf("MCP output=%#v calls=%d", result.Content, toolCalls.Load())
	}
	if out := request(http.MethodPost, models, allCredential.Token, modelBody, 200); !strings.Contains(out, "finished") {
		t.Fatalf("mixed Models result missing: %s", out)
	}
	if out := request(http.MethodGet, searchPath, allCredential.Token, "", 200); !strings.Contains(out, "example.test/answer") {
		t.Fatalf("mixed Search result missing: %s", out)
	}
	activity, err := operatorclient.New(http.DefaultClient, daemon.BaseURL()).Activity(t.Context(), "research")
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Exchanges) < 2 || len(activity.MCPOperations) != 1 {
		t.Fatalf("mixed Activity lost Models/MCP evidence: exchanges=%d mcp=%d", len(activity.Exchanges), len(activity.MCPOperations))
	}
	searchEvidence := daemon.searchEvidence.Events()
	if len(searchEvidence) != 4 {
		t.Fatalf("mixed Activity lost Search attempt evidence: %d", len(searchEvidence))
	}
	for _, event := range searchEvidence {
		if event.Workspace != "research" || event.ConnectionID != "cn_mix_search" || event.ConnectionRevision != "1" {
			t.Fatalf("Search lost current Connection evidence: %#v", event)
		}
	}
	mcpOperation := activity.MCPOperations[0]
	if mcpOperation.Workspace != "research" || mcpOperation.AccessID != all.ID.String() || mcpOperation.Source != "cn_mix_mcp" || mcpOperation.Tool != "search" {
		t.Fatalf("MCP lost Principal/source attribution: %#v", mcpOperation)
	}
	assertContentFree := func(value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, sensitive := range []string{allCredential.Token, modelCredential.Token, searchCredential.Token, "hello", "finished", "mixed MCP result", "example.test/answer"} {
			if strings.Contains(string(raw), sensitive) {
				t.Fatal("execution evidence retained fixture content or a credential")
			}
		}
	}
	assertContentFree(activity)
	assertContentFree(searchEvidence)
	sawModelRoute := false
	for _, record := range activity.Exchanges {
		if record.Workspace != "research" {
			t.Fatalf("Activity crossed Workspace: %#v", record)
		}
		if record.Route != "chat" {
			continue
		}
		sawModelRoute = true
		detail, err := operatorclient.New(http.DefaultClient, daemon.BaseURL()).ActivityOperation(t.Context(), record.ExchangeID)
		if err != nil || detail.Exchange == nil || len(detail.Exchange.Invocations) == 0 {
			t.Fatalf("model route has no causal detail: record=%#v err=%v", record, err)
		}
		assertContentFree(detail)
		foundCurrentBinding := false
		for _, invocation := range detail.Exchange.Invocations {
			if invocation.Route == "chat" && invocation.ConnectionID == "cn_admin" && invocation.ConnectionRevision == 1 {
				foundCurrentBinding = true
			}
		}
		if !foundCurrentBinding {
			t.Fatalf("Activity lost exact route/Connection revision: %#v", detail.Exchange.Invocations)
		}
	}
	if !sawModelRoute {
		t.Fatal("no Models route owner in mixed Activity")
	}
	if toolCalls.Load() != 1 || modelCalls.Load() != 2 || searchCalls.Load() != 2 {
		t.Fatalf("Service effects crossed boundaries: model=%d search=%d tool=%d", modelCalls.Load(), searchCalls.Load(), toolCalls.Load())
	}

	// Readmission on a pre-existing SDK session must use current authority,
	// rather than only the grant that permitted the original MCP handshake.
	setServices(all.ID, "remove-mcp-from-all", []routing.ServiceKind{routing.ServiceModels, routing.ServiceSearch})
	if rejected, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{"query": "mixed"}}); err == nil && rejected != nil && !rejected.IsError {
		t.Fatal("live MCP grant revocation still permitted effect")
	}
	if toolCalls.Load() != 1 {
		t.Fatal("revoked MCP call escaped to upstream")
	}
	request(http.MethodPost, "/workspaces/research/mcp", allCredential.Token, `{}`, 403)
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}

	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon, err = Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	// Both successful Service graphs survive restart; explicit permission changes
	// survive with them, while pre-restart sessions are not reused.
	request(http.MethodPost, models, allCredential.Token, modelBody, 200)
	request(http.MethodGet, searchPath, allCredential.Token, "", 200)
	if modelCalls.Load() != 3 || searchCalls.Load() != 3 || toolCalls.Load() != 1 {
		t.Fatalf("restart changed Service execution: models=%d search=%d mcp=%d", modelCalls.Load(), searchCalls.Load(), toolCalls.Load())
	}
	request(http.MethodPost, "/workspaces/research/mcp", allCredential.Token, `{}`, 403)
	request(http.MethodGet, searchPath, modelCredential.Token, "", 403)
	request(http.MethodPost, models, searchCredential.Token, modelBody, 403)
	if denied, err := connectMCP(allCredential.Token); err == nil {
		_ = denied.Close()
		t.Fatal("restart restored revoked MCP authority")
	}
	if modelCalls.Load() != 3 || searchCalls.Load() != 3 || toolCalls.Load() != 1 {
		t.Fatal("restart denial dispatched an upstream effect")
	}
	historyClient := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	for _, kind := range []string{"exchange", "mcp_operation"} {
		page, err := historyClient.ActivityPage(t.Context(), "research", operatorclient.ActivityPageRequest{Limit: 10, Kind: kind})
		if err != nil || page.Page == nil || page.Page.Mode != "retained" {
			t.Fatalf("restart retained Activity %s unavailable: page=%#v err=%v", kind, page, err)
		}
		assertContentFree(page)
		if kind == "exchange" && len(page.Exchanges) < 2 {
			t.Fatalf("restart lost model execution history: %#v", page.Exchanges)
		}
		if kind == "exchange" {
			for _, original := range activity.Exchanges {
				found := false
				for _, retained := range page.Exchanges {
					if retained.ExchangeID == original.ExchangeID {
						found = true
					}
				}
				if !found {
					t.Fatalf("restart lost original Exchange %s", original.ExchangeID)
				}
			}
		}
		if kind == "mcp_operation" && (len(page.MCPOperations) != 1 || page.MCPOperations[0].OperationID != mcpOperation.OperationID) {
			t.Fatalf("restart lost MCP execution history: %#v", page.MCPOperations)
		}
	}
}
