package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	evidence "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestWorkspaceSelectedToolExecutesAndResumesProvider(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, mixed := range []bool{false, true} {
			t.Run("stream="+strconv.FormatBool(streaming)+"/mixed="+strconv.FormatBool(mixed), func(t *testing.T) {
				key, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
				object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
				decl, _ := canonical.NewFunctionTool(key, "authoritative", canonical.NewToolSchemaObject(object), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
				source, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
				catalog := []mcp.CatalogTool{{Name: "docs__search", SourceID: "source-docs", Source: source, RemoteName: "search", Declaration: decl}}
				providerCalls, effects := 0, 0
				capture := &characterizationEvidenceCapture{}
				runner := withRuntime(func(_ context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
					providerCalls++
					if providerCalls == 1 {
						var wire struct {
							Tools []struct {
								Name string `json:"name"`
							} `json:"tools"`
						}
						if err := json.Unmarshal(document.RawBytes(), &wire); err != nil {
							t.Fatal(err)
						}
						wantTools := 1
						if mixed {
							wantTools = 2
						}
						if len(wire.Tools) != wantTools {
							t.Fatalf("selected declarations missing: %s", document.RawBytes())
						}
						selectedName := ""
						for _, tool := range wire.Tools {
							if tool.Name != "client_tool" {
								selectedName = tool.Name
							}
						}
						raw := `{"id":"round1","model":"m","status":"completed","output":[{"type":"function_call","call_id":"call_selected","name":` + strconv.Quote(selectedName) + `,"arguments":"{}"}]}`
						if mixed {
							raw = `{"id":"round1","model":"m","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"before"}]},{"type":"function_call","call_id":"call_client","name":"client_tool","arguments":"{\"x\":1}"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"between"}]},{"type":"function_call","call_id":"call_selected","name":` + strconv.Quote(selectedName) + `,"arguments":"{}"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"after"}]}]}`
						}
						raw = strings.TrimSuffix(raw, "}") + `,"usage":{"input_tokens":3,"output_tokens":5,"total_tokens":8}}`
						return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(raw), carrier.Meta{})}, nil
					}
					if effects != 1 || !strings.Contains(string(document.RawBytes()), "tool result") || !strings.Contains(string(document.RawBytes()), "call_selected") {
						t.Fatalf("provider resumed without causal result: %s", document.RawBytes())
					}
					if mixed && (!strings.Contains(string(document.RawBytes()), "client result") || !strings.Contains(string(document.RawBytes()), "call_client") || strings.Contains(string(document.RawBytes()), "previous_response_id")) {
						t.Fatalf("mixed resume lost client correlation or omitted unsent local results: %s", document.RawBytes())
					}
					if mixed {
						var replay struct {
							Input []struct {
								Type    string          `json:"type"`
								CallID  string          `json:"call_id"`
								Content json.RawMessage `json:"content"`
							} `json:"input"`
						}
						if err := json.Unmarshal(document.RawBytes(), &replay); err != nil {
							t.Error(err)
							return nil, err
						}
						var order []string
						for _, item := range replay.Input {
							if item.CallID != "" {
								order = append(order, item.Type+":"+item.CallID)
							}
							var text string
							if len(item.Content) > 0 && item.Content[0] == '"' {
								_ = json.Unmarshal(item.Content, &text)
							} else {
								var parts []struct {
									Text string `json:"text"`
								}
								_ = json.Unmarshal(item.Content, &parts)
								for _, part := range parts {
									text += part.Text
								}
							}
							if text == "before" || text == "between" || text == "after" {
								order = append(order, text)
							}
						}
						if strings.Join(order, ",") != "before,function_call:call_client,between,function_call:call_selected,after,function_call_output:call_selected,function_call_output:call_client" {
							t.Errorf("mixed provider replay reordered content: %v; request=%s", order, document.RawBytes())
							return nil, errors.New("mixed replay order mismatch")
						}
					}
					return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"round2","model":"m","output_text":"finished","usage":{"input_tokens":7,"output_tokens":11,"total_tokens":18}}`), carrier.Meta{})}, nil
				})
				runner.ExecutionEvidence = capture
				runner.WorkspaceToolCatalog = func(context.Context, routing.Workspace) ([]mcp.CatalogTool, error) { return catalog, nil }
				transport := runner.Runtime.(testExecutionRuntime).providerTransport
				runner.Runtime = selectedWorkspaceTestRuntime{RuntimeResolver: codecresolver.NewRuntimeCodecResolver(), transport: transport}
				runner.WorkspaceToolCall = func(ctx context.Context, id accessidentity.ID, ws routing.Workspace, frozen mcp.CatalogTool, call canonical.ToolCallItem) (canonical.CanonicalItem, error) {
					if id != accessidentity.Local || ws.Slug().String() != "dev" || frozen.SourceID != "source-docs" {
						t.Fatal("effect authority changed")
					}
					effects++
					return canonical.NewToolResultItem(call.CallID(), []canonical.ToolResultPart{canonical.NewTextToolResultPart("tool result")}, false)
				}
				ingress := RequestIngress{runner: runner}
				store := continuity.NewMemoryStore()
				ingress.runner.CheckpointStore = store
				body := `{"model":"a","input":"use docs","swobu":{"tools":["docs__search"]}}`
				if mixed {
					body = strings.TrimSuffix(body, "}") + `,"tools":[{"type":"function","name":"client_tool","parameters":{"type":"object"}}]}`
				}
				if streaming {
					body = strings.TrimSuffix(body, "}") + `,"stream":true}`
				}
				out, err := ingress.HandleRequestWithWorkspace(t.Context(), requestpathWorkspace(t), RequestInput{AccessID: accessidentity.Local, ClientFamily: clientfamily.Other, ClientRequestID: "selected", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(body)), Operation: canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses}, ResponseFraming: delivery.FramingSSE})
				if err != nil {
					t.Fatal(err)
				}
				raw, err := io.ReadAll(ClientTransportForTest(out.Response).Body)
				if err != nil {
					t.Fatal(err)
				}
				if out.FinalizeExecutionEvidence != nil {
					out.FinalizeExecutionEvidence(delivery.Result{Kind: delivery.Succeeded})
				}
				_ = ClientTransportForTest(out.Response).Body.Close()
				if mixed {
					if providerCalls != 1 || effects != 1 || !strings.Contains(string(raw), "call_client") || strings.Contains(string(raw), "call_selected") {
						t.Fatalf("mixed handoff lost ownership: providers=%d effects=%d body=%s", providerCalls, effects, raw)
					}
					assertSelectedInvocationEvidence(t, capture.events, 1)
					checkpoint, found, err := store.Get(t.Context(), "dev", "swobu_selected")
					if err != nil || !found {
						t.Fatalf("mixed usage checkpoint missing: %v", err)
					}
					assertSelectedResponseUsage(t, checkpoint.Response, 3, 5)
					resume := `{"model":"a","previous_response_id":"swobu_selected","input":[{"type":"function_call_output","call_id":"call_client","output":"client result"}]}`
					for _, wrongID := range []string{"call_selected", "foreign"} {
						invalid := strings.Replace(resume, "call_client", wrongID, 1)
						_, err := ingress.HandleRequestWithWorkspace(t.Context(), requestpathWorkspace(t), RequestInput{AccessID: accessidentity.Local, ClientRequestID: "invalid", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(invalid)), Operation: canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses}, ResponseFraming: delivery.FramingSSE})
						if err == nil || providerCalls != 1 || effects != 1 {
							t.Fatal("client resolved settled local/foreign work or contacted provider")
						}
					}
					if streaming {
						resume = strings.TrimSuffix(resume, "}") + `,"stream":true}`
					}
					resumed, err := ingress.HandleRequestWithWorkspace(t.Context(), requestpathWorkspace(t), RequestInput{AccessID: accessidentity.Local, ClientFamily: clientfamily.Other, ClientRequestID: "resumed", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(resume)), Operation: canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses}, ResponseFraming: delivery.FramingSSE})
					if err != nil {
						t.Fatal(err)
					}
					answer, err := io.ReadAll(ClientTransportForTest(resumed.Response).Body)
					if err != nil {
						t.Fatal(err)
					}
					if resumed.FinalizeExecutionEvidence != nil {
						resumed.FinalizeExecutionEvidence(delivery.Result{Kind: delivery.Succeeded})
					}
					_ = ClientTransportForTest(resumed.Response).Body.Close()
					if providerCalls != 2 || effects != 1 || !strings.Contains(string(answer), "finished") {
						t.Fatalf("resume repeated local work: providers=%d effects=%d body=%s", providerCalls, effects, answer)
					}
					assertSelectedInvocationEvidence(t, capture.events, 2)
					return
				}
				if providerCalls != 2 || effects != 1 || !strings.Contains(string(raw), "finished") {
					t.Fatalf("rounds=%d effects=%d response=%s", providerCalls, effects, raw)
				}
				assertSelectedInvocationEvidence(t, capture.events, 2)
				checkpoint, found, err := store.Get(t.Context(), "dev", "swobu_selected")
				if err != nil || !found {
					t.Fatalf("final checkpoint unavailable: found=%t err=%v", found, err)
				}
				assertSelectedResponseUsage(t, checkpoint.Response, 10, 16)
				var calls, results int
				for _, item := range checkpoint.Request.Items() {
					if call, ok := item.ToolCall(); ok && call.CallID().String() == "call_selected" {
						calls++
					}
					if result, ok := item.ToolResult(); ok && result.CallID().String() == "call_selected" {
						results++
					}
				}
				if calls != 1 || results != 1 {
					t.Fatalf("checkpoint lost causal local history: calls=%d results=%d", calls, results)
				}
				if checkpoint.Response.Response().SwobuID != "swobu_selected" {
					t.Fatal("checkpoint lost public response identity")
				}
			})
		}
	}
}

// Real request-path facts must count physical inference and local effects once,
// never manufacture a child invocation for caller work or replayed history.
func assertSelectedInvocationEvidence(t *testing.T, events []evidence.Event, wantModels int) {
	t.Helper()
	models, tools := 0, 0
	input, output := 0, 0
	starts := make(map[string]bool)
	settled := make(map[string]bool)
	for _, event := range events {
		if start, ok := event.(evidence.InvocationStartedEvent); ok {
			id := start.ID().String()
			if starts[id] {
				t.Fatal("physical invocation started twice")
			}
			starts[id] = true
		}
		if terminal, ok := event.(evidence.InvocationSettledEvent); ok {
			id := terminal.ID().String()
			if !starts[id] || settled[id] {
				t.Fatal("invocation settlement missing start or duplicated")
			}
			settled[id] = true
		}
		switch fact := event.(type) {
		case evidence.ModelInvocationSettled:
			models++
			in, inKnown := fact.Usage().InputTokens()
			out, outKnown := fact.Usage().OutputTokens()
			if !inKnown || !outKnown {
				t.Fatal("known provider usage lost from invocation evidence")
			}
			input += in
			output += out
		case evidence.MCPInvocationSettled:
			tools++
		}
	}
	wantInput, wantOutput := 3, 5
	if wantModels == 2 {
		wantInput, wantOutput = 10, 16
	}
	if models != wantModels || tools != 1 || input != wantInput || output != wantOutput || len(starts) != wantModels+1 || len(settled) != len(starts) {
		t.Fatalf("physical invocation evidence: models=%d tools=%d usage=%d/%d", models, tools, input, output)
	}
}

func assertSelectedResponseUsage(t *testing.T, response canonical.CanonicalResponse, wantInput, wantOutput int) {
	t.Helper()
	input, inKnown := response.Usage().InputTokens()
	output, outKnown := response.Usage().OutputTokens()
	if !inKnown || !outKnown || input != wantInput || output != wantOutput {
		t.Fatalf("checkpoint usage=%d/%d known=%t/%t want=%d/%d", input, output, inKnown, outKnown, wantInput, wantOutput)
	}
}

type selectedWorkspaceTestRuntime struct {
	RuntimeResolver
	transport testProviderTransport
}

func (r selectedWorkspaceTestRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return provider.Backend{Target: target, Codec: protocolcodec.Codec{Protocol: protocolkind.Responses}, Transport: bindTestProviderTransport(target, r.transport)}, nil
}

func TestWorkspaceToolOptInProjectsOnlyFrozenPublishedNames(t *testing.T) {
	key, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object","properties":{"q":{"type":"string"}}}`))
	decl, _ := canonical.NewFunctionTool(key, "authoritative", canonical.NewToolSchemaObject(object), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	sourceDecl, _ := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{decl})
	source, _ := sourceDecl.MCP()
	catalog, _ := mcp.BuildCatalog([]mcp.Mount{{Alias: "docs", Source: source, AllowTools: []string{"search"}}})
	workspace := requestpathWorkspace(t)
	workspaceIngress := RequestIngress{runner: runtimeBundle{WorkspaceToolCatalog: func(context.Context, routing.Workspace) ([]mcp.CatalogTool, error) { return catalog, nil }}}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{}})
	projected, err := workspaceIngress.applyWorkspaceToolOptIn(context.Background(), workspace, request, []byte(`{"swobu":{"tools":["docs__search"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	tools, err := canonical.EffectiveTools(projected)
	if err != nil || len(tools.Declarations()) != 1 {
		t.Fatalf("projected tools=%#v err=%v", tools.Declarations(), err)
	}
	if function, ok := tools.Declarations()[0].Function(); !ok || function.Description() != "authoritative" {
		t.Fatalf("projected declaration=%#v", tools.Declarations()[0])
	}
	if _, err := workspaceIngress.applyWorkspaceToolOptIn(context.Background(), workspace, request, []byte(`{"swobu":{"tools":["docs__delete"]}}`)); err == nil {
		t.Fatal("unknown published tool accepted")
	}
	if _, err := workspaceIngress.applyWorkspaceToolOptIn(context.Background(), workspace, request, []byte(`{"swobu":{"tools":["docs__search","docs__search"]}}`)); err == nil {
		t.Fatal("duplicate tool selection accepted")
	}
	if _, err := workspaceIngress.applyWorkspaceToolOptIn(context.Background(), workspace, request, []byte(`{"swobu":{"tools":["docs__search"],"schema":{"type":"string"}}}`)); err == nil {
		t.Fatal("client-owned schema override accepted")
	}
}

func TestWorkspaceToolOptInRejectsExplicitEmptyOrMalformedSelection(t *testing.T) {
	workspace := requestpathWorkspace(t)
	ingress := RequestIngress{runner: runtimeBundle{WorkspaceToolCatalog: func(context.Context, routing.Workspace) ([]mcp.CatalogTool, error) {
		return nil, nil
	}}}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m")})
	for _, raw := range []string{
		`{"swobu":{"tools":[]}}`,
		`{"swobu":{"tools":null}}`,
		`{"swobu":{"tools":"docs__search"}}`,
		`{"swobu":{"tools":["docs__search"],"extra":true}}`,
	} {
		if _, err := ingress.applyWorkspaceToolOptIn(context.Background(), workspace, request, []byte(raw)); err == nil {
			t.Fatalf("selection %s accepted", raw)
		}
	}
}

func TestWorkspaceToolOptInRejectsNestedClientOwnedNameCollision(t *testing.T) {
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(schemaObject)
	contract := canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled}
	selectedKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	selected, _ := canonical.NewFunctionTool(selectedKey, "trusted", schema, contract)
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	sourceDeclaration, _ := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{selected})
	source, _ := sourceDeclaration.MCP()
	catalog, err := mcp.BuildCatalog([]mcp.Mount{{Alias: "docs", Source: source, AllowTools: []string{"search"}}})
	if err != nil {
		t.Fatal(err)
	}
	ingress := RequestIngress{runner: runtimeBundle{WorkspaceToolCatalog: func(context.Context, routing.Workspace) ([]mcp.CatalogTool, error) { return catalog, nil }}}
	clientKey, _ := canonical.NewToolKey("client/tools", canonical.ToolKindFunction, "docs__search")
	clientFunction, _ := canonical.NewFunctionTool(clientKey, "client-owned", schema, contract)
	namespaceKey, _ := canonical.NewRequestToolKey(canonical.ToolKindNamespace, "client")
	namespace, err := canonical.NewToolNamespace(namespaceKey, "", []canonical.ToolDeclaration{clientFunction})
	if err != nil {
		t.Fatal(err)
	}
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{namespace})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{item}})
	if _, err := ingress.applyWorkspaceToolOptIn(t.Context(), requestpathWorkspace(t), request, []byte(`{"swobu":{"tools":["docs__search"]}}`)); err == nil {
		t.Fatal("nested client-owned tool shadowed selected Workspace name")
	}
}

func TestWorkspaceToolOptInOmissionPreservesRequest(t *testing.T) {
	workspace := requestpathWorkspace(t)
	called := false
	ingress := RequestIngress{runner: runtimeBundle{WorkspaceToolCatalog: func(context.Context, routing.Workspace) ([]mcp.CatalogTool, error) {
		called = true
		return nil, nil
	}}}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m")})
	projected, err := ingress.applyWorkspaceToolOptIn(context.Background(), workspace, request, []byte(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	if called || len(projected.Items()) != len(request.Items()) || projected.Model() != request.Model() {
		t.Fatalf("omitted opt-in changed request: called=%v projected=%#v request=%#v", called, projected, request)
	}
}

func TestHandleRequestWithWorkspaceRequiresAccessID(t *testing.T) {
	workspace := requestpathWorkspace(t)
	ingress := RequestIngress{runner: withRuntime(func(context.Context, provider.TargetSnapshot, carrier.Document) (provider.Ingress, error) {
		t.Fatal("missing AccessID reached provider transport")
		return nil, nil
	})}
	_, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{
		ClientRequestID: "missing-access",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"a","input":"hi"}`)),
		Operation:       canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses},
		ResponseFraming: delivery.FramingSSE,
	})
	if err == nil || !strings.Contains(err.Error(), "access ID is required") {
		t.Fatalf("error = %v, want required AccessID", err)
	}
}

func TestHandleRequestWithEmptyWorkspaceReportsOperationNotConfigured(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("research")
	workspace, err := routing.RestoreWorkspace(slug, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ingress := RequestIngress{runner: withRuntime(func(context.Context, provider.TargetSnapshot, carrier.Document) (provider.Ingress, error) {
		t.Fatal("empty Workspace reached provider transport")
		return nil, nil
	})}
	access := accessidentity.Local
	_, err = ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{
		AccessID:        access,
		ClientRequestID: "empty-workspace",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"a","input":"hi"}`)),
		Operation:       canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses},
		ResponseFraming: delivery.FramingSSE,
	})
	var canonicalErr canonical.Error
	if !errors.As(err, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeOperationNotConfigured {
		t.Fatalf("error=%v, want operation-not-configured", err)
	}
}

func requestpathTarget(t *testing.T, id string) routing.Target {
	t.Helper()
	targetID, _ := routing.ParseTargetID(id)
	model, _ := routing.ParseUpstreamModel("upstream-" + id)
	provider, _ := routing.ParseProvider("custom", func(candidate string) bool { return candidate == "custom" })
	connection, _ := routing.NewCustomConnection(provider, "https://example.test/v1", nil)
	protocol, _ := routing.ParseProtocol("responses", provider, func(routing.Provider, string) bool { return true })
	target, err := routing.NewTarget(targetID, model, protocol, ModelFixtureConnection(t, connection))
	if err != nil {
		t.Fatal(err)
	}
	return target
}
func requestpathWorkspace(t *testing.T) routing.Workspace {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug("dev")
	aName, _ := routing.ParseRouteName("a")
	bName, _ := routing.ParseRouteName("b")
	aTier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "target-a")})
	bTier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "target-b")})
	a, _ := routing.NewRoute(aName, []routing.Tier{aTier})
	b, _ := routing.NewRoute(bName, []routing.Tier{bTier})
	workspace, err := routing.NewWorkspace(slug, []routing.Route{a, b})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func TestRequestPathReachesProviderBeforeItReturns(t *testing.T) {
	workspace := requestpathWorkspace(t)
	release := make(chan struct{})
	providerEntered := make(chan struct{})
	runner := withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		close(providerEntered)
		<-release
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{})}, nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := runLocalExchange(context.Background(), runner, "req_inflight_live", "codex-tui/0.147.0", canonical.ClientFamilyResponses, delivery.BufferedDelivery(), testDecodedRequest(testCanonicalRequest("a")), nil, workspace, nil, canonical.NormalizedPathResponses, executionaffinity.Key{})
		done <- err
	}()

	select {
	case <-providerEntered:
	case <-time.After(time.Second):
		t.Fatal("provider transport was not entered")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRequestPathNeverAttemptsTargetFromAnotherRoute(t *testing.T) {
	workspace := requestpathWorkspace(t)
	var refs []string
	ingress := RequestIngress{runner: withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		refs = append(refs, target.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{})}, nil
	})}
	_, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{AccessID: accessidentity.Local, ClientRequestID: "route-a", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"a","input":"hi"}`)), Operation: canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses}, ResponseFraming: delivery.FramingSSE})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != "target-a" {
		t.Fatalf("attempted targets=%v", refs)
	}
}

func TestRequestPathProjectedRouteCannotEscapeOneRouteWorkspace(t *testing.T) {
	source := requestpathWorkspace(t)
	sharedRouteName, err := routing.ParseRouteName("b")
	if err != nil {
		t.Fatal(err)
	}
	sharedRoute, err := source.ResolveRoute(sharedRouteName.String())
	if err != nil {
		t.Fatal(err)
	}
	projected, err := routing.NewWorkspace(source.Slug(), []routing.Route{sharedRoute})
	if err != nil {
		t.Fatal(err)
	}

	var refs []string
	ingress := RequestIngress{runner: withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		refs = append(refs, target.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{})}, nil
	})}
	_, err = ingress.HandleRequestWithWorkspace(context.Background(), projected, RequestInput{
		AccessID:        accessidentity.Local,
		ClientRequestID: "shared-route",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"a","input":"hi"}`)),
		Operation:       canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses},
		ResponseFraming: delivery.FramingSSE,
	})
	if err == nil {
		t.Fatal("unmatched sibling model was accepted")
	}
	if len(refs) != 0 {
		t.Fatalf("projected route attempted targets=%v, want no target", refs)
	}
}

func TestRequestPathUnmatchedClientModelRejectsUnconfiguredID(t *testing.T) {
	workspace := requestpathWorkspace(t)
	var refs []string
	ingress := RequestIngress{runner: withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		refs = append(refs, target.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{})}, nil
	})}
	_, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{AccessID: accessidentity.Local, ClientRequestID: "fixed-model", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"client-owned-model","input":"hi"}`)), Operation: canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses}, ResponseFraming: delivery.FramingSSE})
	if err == nil {
		t.Fatal("unmatched model used implicit default")
	}
	if len(refs) != 0 {
		t.Fatalf("fixed client model attempted targets=%v, want no target", refs)
	}
}

func TestRequestPathDefaultIsNotImplicitAlias(t *testing.T) {
	workspace := requestpathWorkspace(t)
	var refs []string
	ingress := RequestIngress{runner: withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		refs = append(refs, target.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{})}, nil
	})}
	_, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{AccessID: accessidentity.Local, ClientRequestID: "default", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"default","input":"hi"}`)), Operation: canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses}, ResponseFraming: delivery.FramingSSE})
	if err == nil {
		t.Fatal("unconfigured exact model ID default must fail")
	}
	if len(refs) != 0 {
		t.Fatalf("unexpected provider attempt for unconfigured model ID: %v", refs)
	}
}
