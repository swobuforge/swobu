package exchange

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	providersadapter "github.com/swobuforge/swobu/internal/adapters/outbound/providers"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestGeminiNativeTextStreamServesEveryClientIngress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/interactions" {
			t.Fatalf("method/path = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("x-goog-api-key") != "gemini-token" || request.Header.Get("Authorization") != "" {
			t.Fatalf("Gemini authentication headers = %#v", request.Header)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, geminiTextSSE)
	}))
	defer server.Close()

	registry, err := providersadapter.NewProviderRegistry(server.Client(), geminiTextCredentialResolver{})
	if err != nil {
		t.Fatal(err)
	}
	runtime := geminiTextRuntime{RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(), registry: registry}
	workspace := geminiTextWorkspace(t, server.URL+"/v1")
	ingress := NewIngress(geminiTextWorkspaceLookup{workspace: workspace}, runtime, RuntimePoliciesSpec{
		PolicyResolver: StaticWorkspacePolicyResolver{Policy: DefaultWorkspacePolicy()},
		ResponseIDs:    deterministicResponseIDGenerator{},
	})

	for _, tc := range []struct {
		name   string
		family canonical.ClientFamily
		path   string
		body   string
		want   string
	}{
		{name: "chat", family: canonical.ClientFamilyChatCompletions, path: "/chat/completions", body: `{"model":"gemini-route","stream":true,"messages":[{"role":"user","content":"hello"}]}`, want: "hello from Gemini"},
		{name: "responses", family: canonical.ClientFamilyResponses, path: "/responses", body: `{"model":"gemini-route","stream":true,"input":"hello"}`, want: "hello from Gemini"},
		{name: "messages", family: canonical.ClientFamilyMessages, path: "/messages", body: `{"model":"gemini-route","stream":true,"messages":[{"role":"user","content":"hello"}]}`, want: "hello from Gemini"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{
				ExchangeID: "gemini-" + tc.name, Request: NewTransportRequest(http.MethodPost, tc.path, http.Header{"Content-Type": {"application/json"}}, []byte(tc.body)),
				ClientFamily: tc.family, ResponseFraming: delivery.FramingSSE,
			})
			if err != nil {
				t.Fatal(err)
			}
			response := ClientTransportForTest(out.Response)
			raw, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), tc.want) {
				t.Fatalf("client response missing Gemini text: %s", raw)
			}
		})
	}
}

func TestGeminiDefaultRouteAcceptsBufferedResponsesAfterPortableSearchHistory(t *testing.T) {
	providerRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerRequests++
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte("google_search_call")) || bytes.Contains(body, []byte("google_search_result")) {
			t.Fatalf("settled portable Search history reached Gemini: %s", body)
		}
		if !bytes.Contains(body, []byte(`"text":"verify it"`)) {
			t.Fatalf("current Responses turn missing from Gemini request: %s", body)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, geminiTextSSE)
	}))
	defer server.Close()

	registry, err := providersadapter.NewProviderRegistry(server.Client(), geminiTextCredentialResolver{})
	if err != nil {
		t.Fatal(err)
	}
	runtime := geminiTextRuntime{RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(), registry: registry}
	workspace := geminiTextWorkspace(t, server.URL+"/v1")
	ingress := NewIngress(geminiTextWorkspaceLookup{workspace: workspace}, runtime, RuntimePoliciesSpec{
		PolicyResolver: StaticWorkspacePolicyResolver{Policy: DefaultWorkspacePolicy()},
		ResponseIDs:    deterministicResponseIDGenerator{},
	})
	body := []byte(`{
		"model":"default",
		"stream":false,
		"tools":[{"type":"web_search"}],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"find the deadline"}]},
			{"type":"web_search_call","status":"completed","action":{"type":"search","queries":["deadline"],"sources":[]}},
			{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"July 21"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"verify it"}]}
		]
	}`)
	out, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{
		ExchangeID: "gemini-buffered-portable-search",
		Request: NewTransportRequest(
			http.MethodPost,
			"/responses",
			http.Header{"Content-Type": {"application/json"}},
			body,
		),
		ClientFamily: canonical.ClientFamilyResponses,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := ClientTransportForTest(out.Response)
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if providerRequests != 1 || !strings.Contains(string(raw), "hello from Gemini") {
		t.Fatalf("provider requests = %d response = %s", providerRequests, raw)
	}
}

func TestGeminiResponsesIngressDispatchesWhenParallelToolCallsFalseWithTools(t *testing.T) {
	providerRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerRequests++
		if request.Method != http.MethodPost || request.URL.Path != "/v1/interactions" {
			t.Fatalf("method/path = %s %s", request.Method, request.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "gemini-model" {
			t.Fatalf("model = %#v", payload["model"])
		}
		tools, ok := payload["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools payload = %#v", payload["tools"])
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, geminiTextSSE)
	}))
	defer server.Close()

	registry, err := providersadapter.NewProviderRegistry(server.Client(), geminiTextCredentialResolver{})
	if err != nil {
		t.Fatal(err)
	}
	runtime := geminiTextRuntime{RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(), registry: registry}
	workspace := geminiTextWorkspace(t, server.URL+"/v1")
	ingress := NewIngress(geminiTextWorkspaceLookup{workspace: workspace}, runtime, RuntimePoliciesSpec{
		PolicyResolver: StaticWorkspacePolicyResolver{Policy: DefaultWorkspacePolicy()},
		ResponseIDs:    deterministicResponseIDGenerator{},
	})
	body := []byte(`{
		"model":"default",
		"stream":true,
		"parallel_tool_calls":false,
		"tools":[{
			"type":"function",
			"name":"lookup",
			"description":"lookup function",
			"parameters":{"type":"object","properties":{"q":{"type":"string"}}}
		}],
		"input":"hello"
	}`)
	out, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{
		ExchangeID: "gemini-responses-parallel-tool-calls-false",
		Request: NewTransportRequest(
			http.MethodPost,
			"/responses",
			http.Header{"Content-Type": {"application/json"}},
			body,
		),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("HandleRequestWithWorkspace failed: %v", err)
	}
	response := ClientTransportForTest(out.Response)
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if providerRequests != 1 || !strings.Contains(string(raw), "hello from Gemini") {
		t.Fatalf("provider requests = %d response = %s", providerRequests, raw)
	}
}

// TestGeminiFreshContinuationReentryAfterLocallyResolvedFunctionRound composes
// the exchange/continuity seam with the real Gemini codec. Complete
// canonical call/result history remains durable while Gemini receives only the
// new result after the interaction that emitted the call.
func TestGeminiFreshContinuationReentryAfterLocallyResolvedFunctionRound(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	declaration := canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, declaration),
			canonicaltest.Message(t, canonical.MessageRoleUser, "look it up"),
		},
	})
	prepared, err := continuity.Begin(request)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := canonical.NewToolCallID("call_lookup")
	call := canonicaltest.ToolCall(t, callID.String(), key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := providersadapter.NewProviderRegistry(http.DefaultClient, geminiTextCredentialResolver{})
	if err != nil {
		t.Fatal(err)
	}
	workspace := geminiTextWorkspace(t, "https://generativelanguage.googleapis.com/v1")
	target := workspace.Routes()[0].Tiers()[0].Targets()[0]
	targetSnapshot, err := ProviderTargetFromConnection(target.ID().String(), target.Connection(), target.Protocol().String())
	if err != nil {
		t.Fatal(err)
	}
	fresh := canonical.ResponseRef{SwobuID: "resp_call", Interactions: &canonical.InteractionsContinuation{
		ProviderInteractionID: canonical.NewInteractionID("interaction_call"), TargetID: targetSnapshot.TargetID, TargetVersion: targetSnapshot.TargetVersion,
	}}
	providerResponse, err := canonical.NewCanonicalResponse(fresh, "gemini-model", []canonical.CanonicalItem{call}, canonical.Completed("requires_action"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	prepared, err = prepared.ContinueAfterLocalResult(providerResponse, []canonical.CanonicalItem{result})
	if err != nil {
		t.Fatal(err)
	}
	state := reducerTestState(t)
	state.input.request = request
	state.input.clientFamily = canonical.ClientFamilyResponses
	state.input.clientDelivery = delivery.StreamingDelivery(delivery.FramingSSE)
	state.prepared = &prepared
	state.route = routePlan{targets: []routing.Target{target}}
	runner := withRuntime(bufferedProviderTransport(nil))
	runner.Runtime = geminiTextRuntime{RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(), registry: registry}

	callRequest, _, _, _, err := prepareProviderCall(context.Background(), state, providerCallSelection{candidateIndex: 0, requestChoice: providerRequestPreferred}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if callRequest.request.PreviousHistory == nil || callRequest.request.PreviousHistory.Response.Interactions == nil {
		t.Fatal("Gemini re-entry omitted fresh interaction continuation")
	}
	wire := string(callRequest.document.RawBytes())
	if !strings.Contains(wire, `"previous_interaction_id":"interaction_call"`) || !strings.Contains(wire, `"type":"function_result"`) || strings.Contains(wire, `"type":"function_call"`) {
		t.Fatalf("Gemini re-entry wire = %s", wire)
	}
}

const geminiTextSSE = `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"model_output"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"text","text":"hello from Gemini"}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"completed","usage":{"total_input_tokens":2,"total_output_tokens":3}}}

`

type geminiTextCredentialResolver struct{}

func (geminiTextCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "gemini-token", nil
}

type geminiTextRuntime struct {
	codecresolver.RuntimeCodecResolver
	registry providersadapter.ProviderRegistry
}

func (r geminiTextRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return r.registry.ResolveBackend(target)
}

type geminiTextWorkspaceLookup struct{ workspace routing.Workspace }

func (l geminiTextWorkspaceLookup) GetWorkspace(context.Context, routing.WorkspaceSlug) (routing.Workspace, error) {
	return l.workspace, nil
}

func geminiTextWorkspace(t *testing.T, baseURL string) routing.Workspace {
	t.Helper()
	slug, err := routing.ParseWorkspaceSlug("gemini")
	if err != nil {
		t.Fatal(err)
	}
	routeName, err := routing.ParseRouteName("gemini-route")
	if err != nil {
		t.Fatal(err)
	}
	providerID, err := routing.ParseProvider("gemini", profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := routing.NewStandardConnection(providerID, baseURL, "env:GEMINI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := routing.ParseProtocol("interactions_stream", providerID, func(provider routing.Provider, protocol string) bool {
		return profile.SupportsProviderProtocolForSpec(string(provider), protocol)
	})
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := routing.ParseTargetID("gemini-target")
	model, _ := routing.ParseUpstreamModel("gemini-model")
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	tier, err := routing.NewTier([]routing.Target{target})
	if err != nil {
		t.Fatal(err)
	}
	route, err := routing.NewRoute(routeName, []routing.Tier{tier})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

var _ providersruntime.CredentialProvider = geminiTextCredentialResolver{}
