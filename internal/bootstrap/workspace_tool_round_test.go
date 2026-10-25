package bootstrap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/accessstate"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/execution"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// This is daemon composition proof with synthetic provider HTTP and a trusted
// source binding, not qualification of an external provider or MCP transport.
func TestDaemonWorkspaceSelectedToolRound(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	for _, family := range []string{"responses", "chat_completions", "messages"} {
		t.Run(family, func(t *testing.T) {
			for _, streaming := range []bool{false, true} {
				t.Run(fmt.Sprintf("stream=%t", streaming), func(t *testing.T) {
					for _, scenario := range []string{"authorized", "rate-exhausted", "mixed", "mixed-implicit", "mixed-credential-revoked", "mixed-permission-revoked", "mixed-workspace-restored", "mixed-cancelled-effect", "mixed-effect-failed", "text-then-tool", "gated-text-then-tool", "visible-permission-revoked", "visible-permission-expired", "cancelled-visible-effect", "credential-revoked", "permission-revoked", "workspace-restored", "cancelled-effect", "incremental-answer", "incremental-no-tools"} {
						mode := strings.TrimPrefix(scenario, "mixed-")
						mixed := scenario == "mixed" || strings.HasPrefix(scenario, "mixed-")
						if scenario == "mixed-implicit" {
							if family == "responses" {
								continue
							}
							mode = "mixed"
						}
						if (strings.HasPrefix(mode, "incremental-") || mode == "gated-text-then-tool" || mode == "visible-permission-revoked" || mode == "visible-permission-expired" || mode == "cancelled-visible-effect") && !streaming {
							continue
						}
						t.Run(scenario, func(t *testing.T) {
							visibleTool := mode == "gated-text-then-tool" || mode == "visible-permission-revoked" || mode == "visible-permission-expired" || mode == "cancelled-visible-effect"
							textThenTool := mode == "text-then-tool" || visibleTool
							root := t.TempDir()
							if err := os.Chmod(root, 0o700); err != nil {
								t.Fatal(err)
							}
							t.Setenv(config.EnvSwobuHome, root)
							var providerCalls, effects atomic.Int32
							effectStarted := make(chan struct{})
							effectCancelled := make(chan struct{})
							clientDelta := make(chan struct{})
							var beforeToolResponse func() error
							provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								w.Header().Set("Content-Type", "application/json")
								if streaming {
									w.Header().Set("Content-Type", "text/event-stream")
								}
								raw, err := io.ReadAll(r.Body)
								if err != nil {
									t.Error(err)
									w.WriteHeader(http.StatusInternalServerError)
									return
								}
								path, _ := workspaceToolRoundRequest(family)
								if r.URL.Path != "/v1"+path {
									t.Errorf("provider path=%q", r.URL.Path)
								}
								round := providerCalls.Add(1)
								if round == 1 && mode != "incremental-no-tools" {
									var request struct {
										Tools []struct {
											Name        string          `json:"name"`
											InputSchema json.RawMessage `json:"input_schema"`
											Parameters  json.RawMessage `json:"parameters"`
											Function    struct {
												Name       string          `json:"name"`
												Parameters json.RawMessage `json:"parameters"`
											} `json:"function"`
										} `json:"tools"`
									}
									wantTools := 1
									if mixed {
										wantTools = 2
									}
									if err := json.Unmarshal(raw, &request); err != nil || len(request.Tools) != wantTools {
										t.Errorf("selected declaration missing: %s", raw)
										w.WriteHeader(http.StatusInternalServerError)
										return
									}
									if err := beforeToolResponse(); err != nil {
										t.Error(err)
										w.WriteHeader(http.StatusInternalServerError)
										return
									}
									// Revocation is durable before the tool-call response can be
									// decoded and prepared; no scheduling or sleep race is needed.
									name := request.Tools[0].Name
									schema := request.Tools[0].Parameters
									if family == "chat_completions" {
										name = request.Tools[0].Function.Name
										schema = request.Tools[0].Function.Parameters
									} else if family == "messages" {
										schema = request.Tools[0].InputSchema
									}
									if name == "" {
										t.Error("selected tool wire name is empty")
									}
									var nativeSchema struct {
										Type       string `json:"type"`
										Properties map[string]struct {
											Type string `json:"type"`
										} `json:"properties"`
									}
									if json.Unmarshal(schema, &nativeSchema) != nil || nativeSchema.Type != "object" || nativeSchema.Properties["query"].Type != "string" {
										t.Errorf("native selected schema changed: %s", schema)
									}
									if streaming {
										wire := workspaceToolRoundStream(family, name, false)
										if mixed {
											wire = workspaceMixedToolRound(family, name, true)
										}
										if textThenTool {
											wire = workspaceToolRoundTextThenTool(family, name)
										}
										if visibleTool {
											marker := "event: response.completed"
											if family == "chat_completions" {
												marker = "data: " + fmt.Sprintf(`{"id":"round1","object":"chat.completion.chunk","model":"fixture","choices":[{"index":0,"delta":{"role":"assistant","tool_calls"`)
											} else if family == "messages" {
												marker = "event: content_block_start\ndata: " + `{"type":"content_block_start","index":1`
											}
											prefix, suffix, ok := strings.Cut(wire, marker)
											if !ok {
												t.Error("late tool fixture has no gate boundary")
												return
											}
											_, _ = io.WriteString(w, prefix)
											w.(http.Flusher).Flush()
											select {
											case <-clientDelta:
											case <-time.After(2 * time.Second):
												t.Error("client did not observe text before late local call")
											case <-r.Context().Done():
												return
											}
											if mode == "visible-permission-revoked" || mode == "visible-permission-expired" {
												if err := beforeToolResponse(); err != nil {
													t.Error(err)
												}
											}
											wire = marker + suffix
										}
										_, _ = io.WriteString(w, wire)
									} else {
										wire := workspaceToolRoundResponse(family, name, false)
										if mixed {
											wire = workspaceMixedToolRound(family, name, false)
										}
										if mode == "text-then-tool" {
											switch family {
											case "chat_completions":
												wire = strings.Replace(wire, `"content":null`, `"content":"checking docs"`, 1)
											case "messages":
												wire = strings.Replace(wire, `"content":[`, `"content":[{"type":"text","text":"checking docs"},`, 1)
											default:
												wire = strings.Replace(wire, `"output":[`, `"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"checking docs"}]},`, 1)
											}
										}
										_, _ = io.WriteString(w, wire)
									}
									return
								}
								found := workspaceToolRoundHasResult(family, raw)
								if mode == "mixed" && (!strings.Contains(string(raw), "client result") || !strings.Contains(string(raw), "call_client") || strings.Contains(string(raw), "previous_response_id")) {
									t.Errorf("mixed provider replay lost caller result or used stale native handle: %s", raw)
								}
								if mode != "incremental-no-tools" && ((mode != "authorized" && mode != "mixed" && !textThenTool && mode != "incremental-answer") || effects.Load() != 1 || !found) {
									t.Errorf("invalid provider resume: effects=%d request=%s", effects.Load(), raw)
								}
								if textThenTool && !strings.Contains(string(raw), "checking docs") {
									t.Error("provider continuation lost text before local call")
								}
								if streaming {
									if strings.HasPrefix(mode, "incremental-") {
										wire := workspaceToolRoundStream(family, "", true)
										marker := "event: response.completed"
										if family == "chat_completions" {
											marker = `data: {"id":"round2","object":"chat.completion.chunk","model":"fixture","choices":[{"index":0,"delta":{}`
										} else if family == "messages" {
											marker = "event: content_block_stop"
										}
										prefix, terminal, ok := strings.Cut(wire, marker)
										if !ok {
											t.Error("missing fixture terminal")
											return
										}
										_, _ = io.WriteString(w, prefix)
										w.(http.Flusher).Flush()
										select {
										case <-clientDelta:
										case <-time.After(2 * time.Second):
											t.Error("client did not receive text delta before provider terminal")
										case <-r.Context().Done():
											return
										}
										_, _ = io.WriteString(w, marker+terminal)
										return
									}
									_, _ = io.WriteString(w, workspaceToolRoundStream(family, "", true))
								} else {
									_, _ = io.WriteString(w, workspaceToolRoundResponse(family, "", true))
								}
							}))
							defer provider.Close()
							configPath := filepath.Join(root, "swobu.yaml")
							protocol := family
							if streaming {
								protocol += "_stream"
							}
							writeCurrentInferenceFixture(t, configPath, root, provider.URL, protocol, true)
							schema, err := canonical.ParseJSONObject([]byte(`{"type":"object","properties":{"query":{"type":"string"}}}`))
							if err != nil {
								t.Fatal(err)
							}
							toolKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
							tool, err := canonical.NewFunctionTool(toolKey, "Search docs", canonical.NewToolSchemaObject(schema), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
							if err != nil {
								t.Fatal(err)
							}
							sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
							declaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{tool})
							if err != nil {
								t.Fatal(err)
							}
							source, _ := declaration.MCP()
							publishMCPFixtureConnection(t, configPath, "cn_fixture_docs")
							publishMCPFixtureConnection(t, configPath, "cn_fixture_replacement")
							daemon, err := Start(t.Context(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}, MCPConnectionBindings: map[string]mcp.SourceBinding{
								"cn_fixture_docs": {ID: "cn_fixture_docs", ConnectionID: "cn_fixture_docs", ConnectionRevision: "1", Source: source, Call: func(ctx context.Context, name string, args json.RawMessage) (*sdk.CallToolResult, error) {
									effects.Add(1)
									if mode == "effect-failed" {
										return nil, fmt.Errorf("synthetic source transport failure")
									}
									if _, ok := ctx.Deadline(); !ok {
										t.Error("effect deadline missing")
									}
									if name != "search" || string(args) != `{"query":"docs"}` {
										t.Errorf("wrong binding dispatch: %q %s", name, args)
									}
									if mode == "cancelled-effect" || mode == "cancelled-visible-effect" {
										close(effectStarted)
										<-ctx.Done()
										close(effectCancelled)
										return nil, ctx.Err()
									}
									return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "bound tool result"}}}, nil
								}},
								"cn_fixture_replacement": {ID: "cn_fixture_replacement", ConnectionID: "cn_fixture_replacement", ConnectionRevision: "1", Source: source, Call: func(ctx context.Context, name string, args json.RawMessage) (*sdk.CallToolResult, error) {
									return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "bound tool result"}}}, nil
								}},
							}})
							if err != nil {
								t.Fatal(err)
							}
							defer daemon.Close()
							slug, _ := routing.ParseWorkspaceSlug("research")
							// One available MCP token must cover exactly one composed
							// invocation, independently of catalog and Models admission.
							if mode == "authorized" {
								current, err := daemon.configStore.GetWorkspace(t.Context(), slug)
								if err != nil {
									t.Fatal(err)
								}
								etag := fmt.Sprintf("%s-%d", current.ManagementIncarnation(), current.ManagementRevision())
								_, err = daemon.configStore.SetServiceLimitsChecked(t.Context(), slug, routing.ServiceLimits{Models: routing.ServiceQuota{RequestsPerMinute: 1, Concurrent: 1}, MCP: routing.ServiceQuota{RequestsPerMinute: 1}}, etag, func(context.Context) error { return nil })
								if err != nil {
									t.Fatal(err)
								}
							}
							principal, issued, err := daemon.accessStore.Create("test consumer", slug)
							if err != nil {
								t.Fatal(err)
							}
							sibling, err := daemon.accessStore.IssueCredential(principal.ID, slug)
							if err != nil {
								t.Fatal(err)
							}
							if mode == "rate-exhausted" || mode == "authorized" {
								revision, err := daemon.accessStore.PermissionRevision(principal.ID, slug)
								if err != nil {
									t.Fatal(err)
								}
								_, err = daemon.accessStore.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, slug, revision, "mcp-rate", func() error { return nil }, []routing.ServiceKind{routing.ServiceModels, routing.ServiceMCP}, []accessstate.RequestRateLimit{{Service: routing.ServiceMCP, RequestsPerMinute: 1}})
								if err != nil {
									t.Fatal(err)
								}
								if mode == "rate-exhausted" {
									if _, err := daemon.accessStore.AdmitCredentialOperation(sibling.Credential.ID, slug, routing.ServiceMCP); err != nil {
										t.Fatal(err)
									}
								}
							}
							beforeToolResponse = func() error {
								switch mode {
								case "credential-revoked":
									_, err := daemon.accessStore.RevokeCredential(principal.ID, issued.Credential.ID)
									return err
								case "permission-revoked":
									return daemon.accessStore.RevokePermission(principal.ID, slug)
								case "visible-permission-revoked":
									select {
									case <-clientDelta:
										return daemon.accessStore.RevokePermission(principal.ID, slug)
									default:
										return nil
									}
								case "visible-permission-expired":
									select {
									case <-clientDelta:
										revision := daemon.accessStore.Revision()
										deadline := time.Now().Add(250 * time.Millisecond)
										if err := daemon.accessStore.ApplyPermission(principal.ID, slug, revision, "expire-visible-stream", true, deadline); err != nil {
											return err
										}
										<-time.After(time.Until(deadline) + 5*time.Millisecond)
										return nil
									default:
										return nil
									}
								case "workspace-restored":
									original := daemon.configStore.Config()
									if _, err := daemon.configStore.Update(t.Context(), func(current routing.Config) (routing.Config, error) {
										next, err := current.SetServiceEnabled(slug, routing.ServiceMCP, true)
										if err != nil {
											return routing.Config{}, err
										}
										return next.SetMCPMount(slug, "docs", routing.MCPMount{ConnectionID: "cn_fixture_replacement", AllowedTools: []string{"search"}})
									}); err != nil {
										return err
									}
									_, err := daemon.configStore.Update(t.Context(), func(routing.Config) (routing.Config, error) { return original, nil })
									return err
								default:
									return nil
								}
							}
							path, body := workspaceToolRoundRequest(family)
							if mixed {
								declaration := `{"type":"function","name":"client_tool","parameters":{"type":"object"}}`
								if family == "chat_completions" {
									declaration = `{"type":"function","function":{"name":"client_tool","parameters":{"type":"object"}}}`
								}
								if family == "messages" {
									declaration = `{"name":"client_tool","input_schema":{"type":"object"}}`
								}
								body = strings.TrimSuffix(body, "}") + `,"tools":[` + declaration + `]}`
							}
							if mode == "incremental-no-tools" {
								body = strings.Replace(body, `,"swobu":{"tools":["docs__search"]}`, "", 1)
							}
							if streaming {
								body = strings.TrimSuffix(body, "}") + `,"stream":true}`
							}
							requestCtx, cancelRequest := context.WithCancel(t.Context())
							defer cancelRequest()
							request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, daemon.BaseURL()+"/workspaces/research"+path, strings.NewReader(body))
							if err != nil {
								t.Fatal(err)
							}
							request.Header.Set("Content-Type", "application/json")
							if family == "messages" {
								request.Header.Set("Anthropic-Version", "2023-06-01")
							}
							request.Header.Set("Authorization", "Bearer "+issued.Token)
							client := &http.Client{Timeout: 10 * time.Second}
							if mode == "cancelled-effect" || mode == "cancelled-visible-effect" {
								go func() {
									select {
									case <-effectStarted:
									case <-requestCtx.Done():
										return
									}
									cancelRequest()
								}()
							}
							response, err := client.Do(request)
							if mode == "cancelled-effect" {
								if mixed && streaming && response != nil {
									// Pull caller deltas so backpressure cannot prevent
									// the reducer reaching the effect being cancelled.
									_, _ = io.Copy(io.Discard, response.Body)
								}
								if response != nil {
									_ = response.Body.Close()
								}
								if err == nil && !(mixed && streaming) {
									t.Error("cancelled request returned success")
								}
								select {
								case <-effectCancelled:
								case <-time.After(3 * time.Second):
									t.Fatal("source effect did not observe cancellation")
								}
								if providerCalls.Load() != 1 || effects.Load() != 1 {
									t.Fatalf("cancelled effect resumed: providers=%d effects=%d", providerCalls.Load(), effects.Load())
								}
								return
							}
							if err != nil {
								t.Fatal(err)
							}
							defer response.Body.Close()
							var raw []byte
							if strings.HasPrefix(mode, "incremental-") || visibleTool {
								reader := bufio.NewReader(response.Body)
								observed := false
								for {
									line, readErr := reader.ReadString('\n')
									raw = append(raw, line...)
									if !observed && (strings.Contains(line, "finished") || (visibleTool && strings.Contains(line, "checking docs"))) {
										close(clientDelta)
										observed = true
									}
									if readErr != nil {
										if readErr != io.EOF {
											err = readErr
										}
										break
									}
								}
							} else {
								raw, err = io.ReadAll(response.Body)
							}
							if mode == "authorized" {
								// A different Principal avoids confusing its independent
								// rate policy with the shared Workspace token consumed above.
								_, probe, err := daemon.accessStore.Create("quota probe", slug)
								if err != nil {
									t.Fatal(err)
								}
								session, err := sdk.NewClient(&sdk.Implementation{Name: "composed-quota-proof", Version: "1"}, nil).Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: daemon.BaseURL() + "/workspaces/research/mcp", HTTPClient: &http.Client{Transport: &bearerRoundTripper{next: http.DefaultTransport, token: probe.Token}}, DisableStandaloneSSE: true}, nil)
								if err != nil {
									t.Fatal(err)
								}
								result, callErr := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{"query": "docs"}})
								_ = session.Close()
								if callErr == nil && result != nil && !result.IsError {
									t.Fatal("composed invocation did not consume Workspace MCP credit")
								}
								if effects.Load() != 1 {
									t.Fatalf("denied probe executed: effects=%d", effects.Load())
								}
							}
							if mode == "cancelled-visible-effect" {
								select {
								case <-effectCancelled:
								case <-time.After(3 * time.Second):
									t.Fatal("visible source effect did not observe cancellation")
								}
								if providerCalls.Load() != 1 || effects.Load() != 1 || !strings.Contains(string(raw), "checking docs") || strings.Contains(string(raw), "finished") {
									t.Fatalf("visible cancellation resumed or lost prefix: providers=%d effects=%d body=%s", providerCalls.Load(), effects.Load(), raw)
								}
								return
							}
							if mode == "visible-permission-revoked" {
								if providerCalls.Load() != 1 || effects.Load() != 0 || !strings.Contains(string(raw), "checking docs") || strings.Contains(string(raw), "finished") {
									t.Fatalf("visible revocation resumed or lost prefix: providers=%d effects=%d body=%s", providerCalls.Load(), effects.Load(), raw)
								}
								return
							}
							if mode == "visible-permission-expired" {
								if providerCalls.Load() != 1 || effects.Load() != 0 || !strings.Contains(string(raw), "checking docs") || strings.Contains(string(raw), "finished") {
									t.Fatalf("expired permission admitted a later tool effect or lost stream prefix: providers=%d effects=%d body=%s", providerCalls.Load(), effects.Load(), raw)
								}
								request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, daemon.BaseURL()+"/workspaces/research/v1/models", nil)
								if err != nil {
									t.Fatal(err)
								}
								request.Header.Set("Authorization", "Bearer "+issued.Token)
								fresh, err := http.DefaultClient.Do(request)
								if err != nil {
									t.Fatal(err)
								}
								_ = fresh.Body.Close()
								if fresh.StatusCode != http.StatusUnauthorized {
									t.Fatalf("new request after permission expiry status=%d want=%d", fresh.StatusCode, http.StatusUnauthorized)
								}
								return
							}
							if err != nil {
								t.Fatal(err)
							}
							if mode == "rate-exhausted" {
								if !streaming && (response.StatusCode != 429 || response.Header.Get("Retry-After") == "" || response.Header.Get("Cache-Control") != "no-store") {
									t.Errorf("composed quota response status=%d retry=%q cache=%q", response.StatusCode, response.Header.Get("Retry-After"), response.Header.Get("Cache-Control"))
								}
								if effects.Load() != 0 || providerCalls.Load() != 1 || strings.Contains(string(raw), "finished") {
									t.Fatalf("exhausted MCP quota dispatched effect or resumed provider: effects=%d providers=%d body=%s", effects.Load(), providerCalls.Load(), raw)
								}
								return
							}
							if mixed && mode == "effect-failed" {
								if providerCalls.Load() != 1 || effects.Load() != 1 || strings.Contains(string(raw), "finished") {
									t.Fatalf("mixed failed effect resumed: providers=%d effects=%d body=%s", providerCalls.Load(), effects.Load(), raw)
								}
								if !streaming && response.StatusCode < 400 {
									t.Fatalf("failed effect returned success: %s", raw)
								}
								if streaming && mixedRoundSucceeded(string(raw)) {
									t.Fatalf("failed mixed effect emitted successful terminal: %s", raw)
								}
								return
							}
							if mode == "mixed" {
								if response.StatusCode != http.StatusOK || providerCalls.Load() != 1 || effects.Load() != 1 || !strings.Contains(string(raw), "call_client") || strings.Contains(string(raw), "call_selected") {
									t.Fatalf("mixed handoff failed: status=%d providers=%d effects=%d body=%s", response.StatusCode, providerCalls.Load(), effects.Load(), raw)
								}
								id := regexp.MustCompile(`"id"\s*:\s*"([^"]+)"`).FindStringSubmatch(string(raw))
								if len(id) != 2 {
									t.Fatalf("mixed handoff missing response identity: %s", raw)
								}
								resume := workspaceMixedResume(family, id[1], streaming)
								for _, wrongID := range []string{"call_selected", "foreign"} {
									invalid := strings.Replace(resume, "call_client", wrongID, 1)
									rejected, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/workspaces/research"+path, strings.NewReader(invalid))
									if err != nil {
										t.Fatal(err)
									}
									rejected.Header = request.Header.Clone()
									denied, err := client.Do(rejected)
									if err != nil {
										t.Fatal(err)
									}
									_, _ = io.Copy(io.Discard, denied.Body)
									_ = denied.Body.Close()
									if denied.StatusCode < 400 || providerCalls.Load() != 1 || effects.Load() != 1 {
										t.Fatalf("mixed invalid result admitted: id=%s status=%d providers=%d effects=%d", wrongID, denied.StatusCode, providerCalls.Load(), effects.Load())
									}
								}
								if scenario == "mixed-implicit" {
									resume = workspaceMixedImplicitResume(t, family, raw)
								}
								next, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/workspaces/research"+path, strings.NewReader(resume))
								if err != nil {
									t.Fatal(err)
								}
								next.Header = request.Header.Clone()
								resumed, err := client.Do(next)
								if err != nil {
									t.Fatal(err)
								}
								answer, err := io.ReadAll(resumed.Body)
								_ = resumed.Body.Close()
								if err != nil || resumed.StatusCode != http.StatusOK || providerCalls.Load() != 2 || effects.Load() != 1 || !strings.Contains(string(answer), "finished") {
									t.Fatalf("mixed resume failed: err=%v status=%d providers=%d effects=%d body=%s", err, resumed.StatusCode, providerCalls.Load(), effects.Load(), answer)
								}
								return
							}
							if mode == "authorized" || textThenTool || strings.HasPrefix(mode, "incremental-") {
								if streaming && !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
									t.Error("stream delivery downgraded")
								}
								if streaming {
									terminal := "response.completed"
									if family == "chat_completions" {
										terminal = "[DONE]"
									} else if family == "messages" {
										terminal = "message_stop"
									}
									if !strings.Contains(string(raw), terminal) {
										t.Errorf("missing native success terminal %q: %s", terminal, raw)
									}
									if strings.Count(string(raw), "event: "+terminal) != 1 && family != "chat_completions" {
										t.Errorf("multiple/missing native terminals: %s", raw)
									}
									if family == "chat_completions" && strings.Count(string(raw), "[DONE]") != 1 {
										t.Errorf("multiple/missing Chat terminals: %s", raw)
									}
									if strings.Contains(string(raw), "call_selected") || strings.Contains(string(raw), "docs__search") {
										t.Errorf("Swobu-owned tool leaked as client work: %s", raw)
									}
								}
								wantProviders, wantEffects := int32(2), int32(1)
								if mode == "incremental-no-tools" {
									wantProviders, wantEffects = 1, 0
								}
								if response.StatusCode != http.StatusOK || providerCalls.Load() != wantProviders || effects.Load() != wantEffects || !strings.Contains(string(raw), "finished") {
									t.Fatalf("round failed: status=%d providers=%d effects=%d body=%s", response.StatusCode, providerCalls.Load(), effects.Load(), raw)
								}
								if mode == "authorized" {
									if _, err := daemon.accessStore.AdmitCredentialOperation(sibling.Credential.ID, slug, routing.ServiceMCP); !errors.Is(err, accessstate.ErrRequestRateLimited) {
										t.Fatalf("composed effect failed to debit MCP: %v", err)
									}
									client := operatorclient.New(http.DefaultClient, daemon.BaseURL())
									deadline := time.Now().Add(3 * time.Second)
									var before operatorclient.ActivityProjection
									for {
										before, err = client.Activity(t.Context(), "research")
										if err != nil {
											t.Fatal(err)
										}
										if len(before.Exchanges) == 1 && before.Exchanges[0].State == "succeeded" {
											break
										}
										if time.Now().After(deadline) {
											t.Fatalf("tool-loop did not settle: %#v", before)
										}
										time.Sleep(time.Millisecond)
									}
									if before.Exchanges[0].AttemptCount != 2 || len(before.MCPOperations) != 0 {
										t.Fatalf("tool-loop invented standalone traffic or lost attempts: %#v", before)
									}
									liveLifecycles := daemon.executionEvidenceStore.Settled()
									if len(liveLifecycles) != 1 {
										t.Fatalf("live tool-loop lifecycles=%d", len(liveLifecycles))
									}
									if err := daemon.Close(); err != nil {
										t.Fatal(err)
									}
									if health := daemon.ledgerWriter.Health(); health.CommittedExchangesTotal != 1 || health.DroppedExchangesTotal != 0 || health.CommittedMCPOperationsTotal != 0 {
										t.Fatalf("tool-loop capture=%#v", health)
									}
									restarted, err := Start(t.Context(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}, MCPConnectionBindings: map[string]mcp.SourceBinding{
										"cn_fixture_docs": {ID: "cn_fixture_docs", ConnectionID: "cn_fixture_docs", ConnectionRevision: "1", Source: source, Call: func(context.Context, string, json.RawMessage) (*sdk.CallToolResult, error) {
											return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "bound tool result"}}}, nil
										}},
										"cn_fixture_replacement": {ID: "cn_fixture_replacement", ConnectionID: "cn_fixture_replacement", ConnectionRevision: "1", Source: source, Call: func(context.Context, string, json.RawMessage) (*sdk.CallToolResult, error) {
											return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "bound tool result"}}}, nil
										}},
									}})
									if err != nil {
										t.Fatal(err)
									}
									defer restarted.Close()
									after, err := operatorclient.New(http.DefaultClient, restarted.BaseURL()).Activity(t.Context(), "research")
									if err != nil || !reflect.DeepEqual(before.Exchanges, after.Exchanges) || len(after.MCPOperations) != 0 {
										t.Fatalf("tool-loop restart changed Activity: before=%#v after=%#v err=%v", before, after, err)
									}
									identity, err := execution.ParseExchangeID(after.Exchanges[0].ExchangeID)
									if err != nil {
										t.Fatal(err)
									}
									completed, found, err := restarted.ledgerStore.GetExchange(t.Context(), identity)
									if err != nil || !found || len(completed.Invocations()) != 3 {
										t.Fatalf("durable tool-loop lifecycle=%#v found=%v err=%v", completed, found, err)
									}
									models, tools := 0, 0
									for index, invocation := range completed.Invocations() {
										live := liveLifecycles[0].Invocations[index]
										if invocation.Started().ID() != live.Started.ID() || !invocation.Started().StartedAt().Equal(live.Started.StartedAt()) || !invocation.Settled().SettledAt().Equal(live.Settled.SettledAt()) || invocation.Settled().Outcome() != live.Settled.Outcome() || invocation.Settled().Occurrence() != live.Settled.Occurrence() {
											t.Fatalf("durable invocation changed lifecycle facts: %#v", invocation)
										}
										switch restored := invocation.Started().(type) {
										case domain.ModelInvocationStarted:
											original, ok := live.Started.(domain.ModelInvocationStarted)
											terminal := invocation.Settled().(domain.ModelInvocationSettled)
											originalTerminal := live.Settled.(domain.ModelInvocationSettled)
											if !ok || restored.Model() != original.Model() || terminal.Usage() != originalTerminal.Usage() || terminal.Failure() != originalTerminal.Failure() {
												t.Fatal("durable model resource, usage or failure changed")
											}
										case domain.MCPInvocationStarted:
											original, ok := live.Started.(domain.MCPInvocationStarted)
											if !ok || restored.MCP() != original.MCP() || invocation.Settled().(domain.MCPInvocationSettled).Failure() != live.Settled.(domain.MCPInvocationSettled).Failure() {
												t.Fatal("durable MCP resource or failure changed")
											}
										}
										if invocation.Settled().Outcome() != execution.Succeeded || invocation.Settled().Occurrence() != execution.Occurred {
											t.Fatalf("tool-loop terminal truth=%#v", invocation)
										}
										switch invocation.Started().(type) {
										case domain.ModelInvocationStarted:
											models++
										case domain.MCPInvocationStarted:
											tools++
										}
									}
									if models != 2 || tools != 1 || len(restarted.executionEvidenceStore.Settled()) != 0 {
										t.Fatalf("restart used memory or lost invocation kinds: model=%d tool=%d", models, tools)
									}
									other, err := operatorclient.New(http.DefaultClient, restarted.BaseURL()).Activity(t.Context(), "other")
									if err != nil || len(other.Exchanges) != 0 || len(other.MCPOperations) != 0 {
										t.Fatalf("tool-loop history crossed Workspace: %#v, %v", other, err)
									}
									if providerCalls.Load() != 2 || effects.Load() != 1 {
										t.Fatalf("history read replayed executable work: providers=%d tools=%d", providerCalls.Load(), effects.Load())
									}
								}
							} else {
								// Caller-owned deltas may precede denied local work. Once
								// visible, failure is terminal stream truth, not HTTP status.
								visibleMixedFailure := mixed && streaming && strings.Contains(string(raw), `"error"`) && !mixedRoundSucceeded(string(raw))
								if (response.StatusCode < 400 && !visibleMixedFailure) || providerCalls.Load() != 1 || effects.Load() != 0 || strings.Contains(string(raw), "finished") {
									t.Fatalf("revoked round succeeded: status=%d providers=%d effects=%d body=%s", response.StatusCode, providerCalls.Load(), effects.Load(), raw)
								}
								if mode == "credential-revoked" {
									if _, err := daemon.accessStore.Authenticate(sibling.Token); err != nil {
										t.Fatal("active sibling was revoked")
									}
								}
							}
						})
					}
				})
			}
		})
	}
}

// Chat's DONE closes both failed and successful streams. Successful canonical
// finish evidence, not that framing sentinel, distinguishes terminal truth.
func mixedRoundSucceeded(raw string) bool {
	return strings.Contains(raw, "event: response.completed") || strings.Contains(raw, "event: message_stop") || strings.Contains(raw, `"finish_reason":"tool_calls"`) || strings.Contains(raw, `"finish_reason":"stop"`)
}

// A text prefix cannot establish that the rest of the round is client-owned.
// Keep this witness beside the gated incrementality regression so streaming
// repairs must retain a local call appearing after visible text.
func workspaceToolRoundTextThenTool(family, name string) string {
	toolRound := workspaceToolRoundStream(family, name, false)
	switch family {
	case "chat_completions":
		prefix := "data: " + `{"id":"round1","object":"chat.completion.chunk","model":"fixture","choices":[{"index":0,"delta":{"role":"assistant","content":"checking docs"},"finish_reason":null}]}` + "\n\n"
		return prefix + toolRound
	case "messages":
		start, rest, _ := strings.Cut(toolRound, "event: content_block_start")
		text := "event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\nevent: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"checking docs"}}` + "\n\nevent: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n"
		return start + text + "event: content_block_start" + strings.ReplaceAll(rest, `"index":0`, `"index":1`)
	default:
		start, rest, _ := strings.Cut(toolRound, "event: response.completed")
		text := "event: response.output_text.delta\ndata: " + `{"type":"response.output_text.delta","item_id":"prefix","output_index":0,"content_index":0,"delta":"checking docs"}` + "\n\n"
		rest = strings.Replace(rest, `"output":[`, `"output":[{"id":"prefix","type":"message","role":"assistant","content":[{"type":"output_text","text":"checking docs"}]},`, 1)
		return start + text + "event: response.completed" + rest
	}
}

// Mixed witnesses use each family's native caller-tool representation while
// keeping the existing selected-tool provider round and daemon composition.
func workspaceMixedToolRound(family, name string, streaming bool) string {
	if !streaming {
		raw := workspaceToolRoundResponse(family, name, false)
		switch family {
		case "chat_completions":
			return strings.Replace(raw, `"tool_calls":[`, `"tool_calls":[{"id":"call_client","type":"function","function":{"name":"client_tool","arguments":"{}"}},`, 1)
		case "messages":
			return strings.Replace(raw, `"content":[`, `"content":[{"type":"tool_use","id":"call_client","name":"client_tool","input":{}},`, 1)
		default:
			return strings.Replace(raw, `"output":[`, `"output":[{"type":"function_call","call_id":"call_client","name":"client_tool","arguments":"{}"},`, 1)
		}
	}
	raw := workspaceToolRoundStream(family, name, false)
	switch family {
	case "chat_completions":
		return strings.Replace(raw, `"tool_calls":[`, `"tool_calls":[{"index":1,"id":"call_client","type":"function","function":{"name":"client_tool","arguments":"{}"}},`, 1)
	case "messages":
		start, rest, _ := strings.Cut(raw, "event: content_block_start")
		caller := "event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_client","name":"client_tool","input":{}}}` + "\n\nevent: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}` + "\n\nevent: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n"
		return start + caller + "event: content_block_start" + strings.ReplaceAll(rest, `"index":0`, `"index":1`)
	default:
		return strings.Replace(raw, workspaceToolRoundResponse(family, name, false), workspaceMixedToolRound(family, name, false), 1)
	}
}

func workspaceMixedResume(family, id string, streaming bool) string {
	items := `"input":[{"type":"function_call_output","call_id":"call_client","output":"client result"}]`
	if family == "chat_completions" {
		items = `"messages":[{"role":"tool","tool_call_id":"call_client","content":"client result"}]`
	} else if family == "messages" {
		items = `"max_tokens":100,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_client","content":"client result"}]}]`
	}
	return fmt.Sprintf(`{"model":"chat","previous_response_id":%q,"stream":%t,%s}`, id, streaming, items)
}

func workspaceMixedImplicitResume(t *testing.T, family string, raw []byte) string {
	t.Helper()
	if strings.HasPrefix(string(raw), "event:") || strings.HasPrefix(string(raw), "data:") {
		// Reconstruct the native assistant tool contribution from delivered SSE,
		// just as a client must before appending its result to messages history.
		var id, name, arguments string
		for _, line := range strings.Split(string(raw), "\n") {
			data, ok := strings.CutPrefix(line, "data: ")
			if !ok || data == "[DONE]" {
				continue
			}
			var event struct {
				ContentBlock struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
				Delta struct {
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
				Choices []struct {
					Delta struct {
						ToolCalls []struct {
							ID       string `json:"id"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				t.Fatal(err)
			}
			if event.ContentBlock.ID != "" {
				id, name = event.ContentBlock.ID, event.ContentBlock.Name
			}
			arguments += event.Delta.PartialJSON
			for _, choice := range event.Choices {
				for _, call := range choice.Delta.ToolCalls {
					if call.ID != "" {
						id = call.ID
					}
					if call.Function.Name != "" {
						name = call.Function.Name
					}
					arguments += call.Function.Arguments
				}
			}
		}
		if id != "call_client" || name != "client_tool" || !json.Valid([]byte(arguments)) {
			t.Fatalf("invalid delivered caller tool: id=%s name=%s args=%s", id, name, arguments)
		}
		if family == "chat_completions" {
			raw = []byte(fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}}]}`, id, name, arguments))
		} else {
			raw = []byte(fmt.Sprintf(`{"content":[{"type":"tool_use","id":%q,"name":%q,"input":%s}]}`, id, name, arguments))
		}
	}
	var response struct {
		Content json.RawMessage `json:"content"`
		Choices []struct {
			Message json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	assistant := `{"role":"assistant","content":` + string(response.Content) + `}`
	result := `{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_client","content":"client result"}]}`
	if family == "chat_completions" {
		if len(response.Choices) != 1 {
			t.Fatal("missing Chat assistant history")
		}
		assistant = string(response.Choices[0].Message)
		result = `{"role":"tool","tool_call_id":"call_client","content":"client result"}`
	}
	return `{"model":"chat","max_tokens":100,"messages":[{"role":"user","content":"use docs"},` + assistant + `,` + result + `]}`
}

func workspaceToolRoundStream(family, name string, final bool) string {
	switch family {
	case "chat_completions":
		if final {
			return "data: " + `{"id":"round2","object":"chat.completion.chunk","model":"fixture","choices":[{"index":0,"delta":{"role":"assistant","content":"finished"},"finish_reason":null}]}` + "\n\ndata: " + `{"id":"round2","object":"chat.completion.chunk","model":"fixture","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
		}
		return "data: " + fmt.Sprintf(`{"id":"round1","object":"chat.completion.chunk","model":"fixture","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_selected","type":"function","function":{"name":%q,"arguments":"{\"query\":\"docs\"}"}}]},"finish_reason":null}]}`, name) + "\n\ndata: " + `{"id":"round1","object":"chat.completion.chunk","model":"fixture","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"
	case "messages":
		start := "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"round1","type":"message","role":"assistant","model":"fixture","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n"
		block, delta, reason := fmt.Sprintf(`{"type":"tool_use","id":"call_selected","name":%q,"input":{}}`, name), `{"type":"input_json_delta","partial_json":"{\"query\":\"docs\"}"}`, "tool_use"
		if final {
			block, delta, reason = `{"type":"text","text":""}`, `{"type":"text_delta","text":"finished"}`, "end_turn"
		}
		return start + "event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":` + block + "}\n\n" + "event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":` + delta + "}\n\n" + "event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\nevent: message_delta\ndata: " + fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q},"usage":{"output_tokens":1}}`, reason) + "\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	default:
		start := "event: response.created\ndata: " + `{"type":"response.created","response":{"id":"round1","model":"fixture","status":"in_progress","output":[]}}` + "\n\n"
		if final {
			return start + "event: response.output_text.delta\ndata: " + `{"type":"response.output_text.delta","item_id":"msg1","output_index":0,"content_index":0,"delta":"finished"}` + "\n\nevent: response.completed\ndata: " + `{"type":"response.completed","response":{"id":"round1","model":"fixture","status":"completed","output":[{"id":"msg1","type":"message","role":"assistant","content":[{"type":"output_text","text":"finished"}]}]}}` + "\n\n"
		}
		return start + "event: response.completed\ndata: " + `{"type":"response.completed","response":` + workspaceToolRoundResponse(family, name, false) + "}\n\n"
	}
}

func workspaceToolRoundRequest(family string) (string, string) {
	switch family {
	case "chat_completions":
		return "/chat/completions", `{"model":"chat","messages":[{"role":"user","content":"use docs"}],"swobu":{"tools":["docs__search"]}}`
	case "messages":
		return "/messages", `{"model":"chat","max_tokens":100,"messages":[{"role":"user","content":"use docs"}],"swobu":{"tools":["docs__search"]}}`
	default:
		return "/responses", `{"model":"chat","input":"use docs","swobu":{"tools":["docs__search"]}}`
	}
}

func workspaceToolRoundResponse(family, name string, final bool) string {
	switch family {
	case "chat_completions":
		if final {
			return `{"id":"round2","object":"chat.completion","model":"fixture","choices":[{"index":0,"message":{"role":"assistant","content":"finished"},"finish_reason":"stop"}]}`
		}
		return fmt.Sprintf(`{"id":"round1","object":"chat.completion","model":"fixture","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_selected","type":"function","function":{"name":%q,"arguments":"{\"query\":\"docs\"}"}}]},"finish_reason":"tool_calls"}]}`, name)
	case "messages":
		if final {
			return `{"id":"round2","type":"message","role":"assistant","model":"fixture","content":[{"type":"text","text":"finished"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
		}
		return fmt.Sprintf(`{"id":"round1","type":"message","role":"assistant","model":"fixture","content":[{"type":"tool_use","id":"call_selected","name":%q,"input":{"query":"docs"}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`, name)
	default:
		if final {
			return `{"id":"round2","model":"fixture","output_text":"finished"}`
		}
		return fmt.Sprintf(`{"id":"round1","model":"fixture","status":"completed","output":[{"type":"function_call","call_id":"call_selected","name":%q,"arguments":"{\"query\":\"docs\"}"}]}`, name)
	}
}

// Check the native result location and causal ID, not merely matching text in
// a request that might contain a result under the wrong owner or history item.
func workspaceToolRoundHasResult(family string, raw []byte) bool {
	var request struct {
		Input []struct {
			Type   string          `json:"type"`
			CallID string          `json:"call_id"`
			Output json.RawMessage `json:"output"`
		} `json:"input"`
		Messages []struct {
			Role       string          `json:"role"`
			ToolCallID string          `json:"tool_call_id"`
			Content    json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &request) != nil {
		return false
	}
	if family == "responses" {
		for _, item := range request.Input {
			if item.Type == "function_call_output" && item.CallID == "call_selected" && strings.Contains(string(item.Output), "bound tool result") {
				return true
			}
		}
	}
	for _, message := range request.Messages {
		if family == "chat_completions" && message.Role == "tool" && message.ToolCallID == "call_selected" && strings.Contains(string(message.Content), "bound tool result") {
			return true
		}
		if family == "messages" && message.Role == "user" {
			var blocks []struct {
				Type      string          `json:"type"`
				ToolUseID string          `json:"tool_use_id"`
				Content   json.RawMessage `json:"content"`
			}
			if json.Unmarshal(message.Content, &blocks) != nil {
				continue
			}
			for _, block := range blocks {
				if block.Type == "tool_result" && block.ToolUseID == "call_selected" && strings.Contains(string(block.Content), "bound tool result") {
					return true
				}
			}
		}
	}
	return false
}
