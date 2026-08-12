package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestMessagesReasoningBudgetCompletesThroughZAIWithApproximation(t *testing.T) {
	var calls atomic.Int32
	var providerRequest map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := json.NewDecoder(r.Body).Decode(&providerRequest); err != nil {
			t.Errorf("decode Z.AI request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"glm\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"glm\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	registry := mustProviderRegistry(t, rewritingClientForServer(t, upstream), testCredentialResolver{})
	runtime := reasoningRequestPathRuntime{
		RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(),
		ProviderRegistry:     registry,
	}
	workspace := zaiReasoningWorkspace(t)
	ingress := exchange.NewIngress(nil, runtime, exchange.RuntimePoliciesSpec{})

	raw := []byte(`{
		"model":"reasoning-route",
		"max_tokens":12000,
		"messages":[{"role":"user","content":"hi"}],
		"thinking":{"type":"enabled","budget_tokens":10000}
	}`)
	out, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, exchange.RequestInput{
		ExchangeID:      "zai-reasoning-budget",
		Request:         exchange.NewTransportRequest(http.MethodPost, "/messages", nil, raw),
		ClientFamily:    canonical.ClientFamilyMessages,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		if strings.Contains(err.Error(), string(canonical.ErrorCodeNoCompatibleTarget)) {
			t.Fatalf("request reached NO_COMPATIBLE_TARGET: %v", err)
		}
		t.Fatalf("HandleRequestWithWorkspace: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("Z.AI calls = %d, want 1", calls.Load())
	}
	if providerRequest["reasoning_effort"] != "medium" {
		t.Fatalf("Z.AI reasoning_effort = %#v, want medium; request = %#v", providerRequest["reasoning_effort"], providerRequest)
	}
	thinking, ok := providerRequest["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("Z.AI thinking = %#v, want enabled; request = %#v", providerRequest["thinking"], providerRequest)
	}
	if out.Compatibility == nil {
		t.Fatal("winning exchange compatibility is absent")
	}
	switch response := out.Response.(type) {
	case exchange.BufferedResponse:
		body, readErr := io.ReadAll(response.Response.Body)
		if readErr != nil {
			t.Fatalf("consume successful buffered Messages response: %v", readErr)
		}
		if !strings.Contains(string(body), `"type":"message"`) || !strings.Contains(string(body), `"text":"ok"`) {
			t.Fatalf("Messages response = %s, want successful message", body)
		}
	case exchange.MessageStreamingResponse:
		for {
			_, nextErr := response.Response.Messages.Next(context.Background())
			if errors.Is(nextErr, io.EOF) {
				break
			}
			if nextErr != nil {
				t.Fatalf("consume successful Messages response: %v", nextErr)
			}
		}
	case exchange.StreamingResponse:
		if _, err := io.ReadAll(response.Response.Body); err != nil {
			t.Fatalf("consume byte stream response: %v", err)
		}
	default:
		t.Fatalf("response = %T, want successful Messages response", out.Response)
	}
	snapshot := out.Compatibility.Snapshot()
	if snapshot.State != wire.CompletionCompleted {
		t.Fatalf("compatibility completion state = %v, want completed", snapshot.State)
	}
	if snapshot.Compatibility.Classification != compat.ClassificationApproximate {
		t.Fatalf("compatibility = %#v, want approximate", snapshot.Compatibility)
	}
	if len(snapshot.Compatibility.Changes) != 1 {
		t.Fatalf("compatibility changes = %#v, want exactly one", snapshot.Compatibility.Changes)
	}
	change := snapshot.Compatibility.Changes[0]
	if change.Capability != canonical.RequestReasoning ||
		change.Kind != compat.Approximation ||
		change.Preserved != canonical.RequestControlsEffort {
		t.Fatalf("compatibility change = %#v, want reasoning-to-effort approximation", change)
	}
}

func TestResponsesExplicitEffortCompletesThroughZAIExactly(t *testing.T) {
	var providerRequest map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&providerRequest); err != nil {
			t.Errorf("decode Z.AI request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"glm\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"glm\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	registry := mustProviderRegistry(t, rewritingClientForServer(t, upstream), testCredentialResolver{})
	runtime := reasoningRequestPathRuntime{
		RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(),
		ProviderRegistry:     registry,
	}
	ingress := exchange.NewIngress(nil, runtime, exchange.RuntimePoliciesSpec{})
	raw := []byte(`{
		"model":"reasoning-route",
		"input":"hi",
		"reasoning":{"effort":"high"},
		"stream":true
	}`)
	out, err := ingress.HandleRequestWithWorkspace(context.Background(), zaiReasoningWorkspace(t), exchange.RequestInput{
		ExchangeID:      "zai-responses-explicit-effort",
		Request:         exchange.NewTransportRequest(http.MethodPost, "/responses", nil, raw),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("HandleRequestWithWorkspace: %v", err)
	}
	thinking, ok := providerRequest["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("Z.AI thinking = %#v, want enabled; request = %#v", providerRequest["thinking"], providerRequest)
	}
	if providerRequest["reasoning_effort"] != "high" {
		t.Fatalf("Z.AI reasoning_effort = %#v, want high; request = %#v", providerRequest["reasoning_effort"], providerRequest)
	}
	consumeReasoningResponse(t, out.Response)
	if out.Compatibility == nil {
		t.Fatal("winning exchange compatibility is absent")
	}
	snapshot := out.Compatibility.Snapshot()
	if snapshot.State != wire.CompletionCompleted || snapshot.Compatibility.Classification != compat.ClassificationExact {
		t.Fatalf("compatibility = %#v state=%v, want exact completed", snapshot.Compatibility, snapshot.State)
	}
}

func TestResponsesFunctionResultCompletesThroughZAIChatBridge(t *testing.T) {
	var providerRequests []map[string]any
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode Z.AI request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		providerRequests = append(providerRequests, request)
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_tool\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"glm\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_tool\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"glm\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_final\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"glm\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"sunny\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_final\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"glm\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	registry := mustProviderRegistry(t, rewritingClientForServer(t, upstream), testCredentialResolver{})
	runtime := reasoningRequestPathRuntime{
		RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(),
		ProviderRegistry:     registry,
	}
	ingress := exchange.NewIngress(nil, runtime, exchange.RuntimePoliciesSpec{})
	workspace := zaiReasoningWorkspace(t)

	first := []byte(`{
		"model":"reasoning-route",
		"input":"weather?",
		"max_output_tokens":64,
		"reasoning":{"effort":"high"},
		"tools":[{"type":"function","name":"lookup","description":"lookup","parameters":{"type":"object"}}],
		"stream":true
	}`)
	firstOut, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, exchange.RequestInput{
		ExchangeID:      "zai-responses-function-call",
		Request:         exchange.NewTransportRequest(http.MethodPost, "/responses", nil, first),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("first HandleRequestWithWorkspace: %v", err)
	}
	firstBody := readReasoningResponse(t, firstOut.Response)
	if !strings.Contains(firstBody, `"name":"lookup"`) || !strings.Contains(firstBody, `"call_id":"call_1"`) {
		t.Fatalf("Responses tool call lost identity: %s", firstBody)
	}

	second := []byte(`{
		"model":"reasoning-route",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"weather?"}]},
			{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"{\"temperature\":21}"}
		],
		"max_output_tokens":64,
		"reasoning":{"effort":"high"},
		"tools":[{"type":"function","name":"lookup","description":"lookup","parameters":{"type":"object"}}],
		"stream":true
	}`)
	secondOut, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, exchange.RequestInput{
		ExchangeID:      "zai-responses-function-result",
		Request:         exchange.NewTransportRequest(http.MethodPost, "/responses", nil, second),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("second HandleRequestWithWorkspace: %v", err)
	}
	secondBody := readReasoningResponse(t, secondOut.Response)
	if !strings.Contains(secondBody, "sunny") {
		t.Fatalf("Responses final answer = %s, want sunny", secondBody)
	}

	if len(providerRequests) != 2 {
		t.Fatalf("Z.AI requests = %d, want 2", len(providerRequests))
	}
	assertZAIFunctionRequest(t, providerRequests[0], false)
	assertZAIFunctionRequest(t, providerRequests[1], true)
}

func assertZAIFunctionRequest(t *testing.T, request map[string]any, wantResult bool) {
	t.Helper()
	thinking, ok := request["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" || request["reasoning_effort"] != "high" {
		t.Fatalf("Z.AI reasoning fields = thinking:%#v effort:%#v", request["thinking"], request["reasoning_effort"])
	}
	if request["max_tokens"] != float64(64) {
		t.Fatalf("Z.AI max_tokens = %#v, want 64", request["max_tokens"])
	}
	tools, ok := request["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("Z.AI tools = %#v, want one function", request["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	function, functionOK := tool["function"].(map[string]any)
	if !ok || !functionOK || tool["type"] != "function" || function["name"] != "lookup" {
		t.Fatalf("Z.AI function tool = %#v", tools[0])
	}
	if !wantResult {
		return
	}
	messages, ok := request["messages"].([]any)
	if !ok {
		t.Fatalf("Z.AI messages = %#v", request["messages"])
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if ok && message["role"] == "tool" && message["tool_call_id"] == "call_1" && strings.Contains(message["content"].(string), "21") {
			return
		}
	}
	t.Fatalf("Z.AI messages lost function result: %#v", messages)
}

func readReasoningResponse(t *testing.T, response any) string {
	t.Helper()
	switch response := response.(type) {
	case exchange.BufferedResponse:
		body, err := io.ReadAll(response.Response.Body)
		if err != nil {
			t.Fatalf("consume buffered response: %v", err)
		}
		return string(body)
	case exchange.StreamingResponse:
		body, err := io.ReadAll(response.Response.Body)
		if err != nil {
			t.Fatalf("consume byte stream response: %v", err)
		}
		return string(body)
	default:
		t.Fatalf("response = %T, want Responses response", response)
		return ""
	}
}

func consumeReasoningResponse(t *testing.T, response any) {
	t.Helper()
	switch response := response.(type) {
	case exchange.BufferedResponse:
		if _, err := io.ReadAll(response.Response.Body); err != nil {
			t.Fatalf("consume buffered response: %v", err)
		}
	case exchange.MessageStreamingResponse:
		for {
			_, err := response.Response.Messages.Next(context.Background())
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				t.Fatalf("consume streaming response: %v", err)
			}
		}
	case exchange.StreamingResponse:
		if _, err := io.ReadAll(response.Response.Body); err != nil {
			t.Fatalf("consume byte stream response: %v", err)
		}
	default:
		t.Fatalf("response = %T, want successful response", response)
	}
}

type reasoningRequestPathRuntime struct {
	codecresolver.RuntimeCodecResolver
	ProviderRegistry
}

func zaiReasoningWorkspace(t *testing.T) routing.Workspace {
	t.Helper()

	targetID, err := routing.ParseTargetID("zai-target")
	if err != nil {
		t.Fatal(err)
	}
	model, err := routing.ParseUpstreamModel("glm")
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := routing.ParseProvider("zai", profile.SupportsSpec)
	connection, err := routing.NewZAIConnection(provider, routing.ZAIAccessGeneralAPI, "env:ZAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := routing.ParseProtocol(routing.ZAIProviderProtocol, provider, func(candidateProvider routing.Provider, candidate string) bool {
		return candidateProvider == provider && candidate == routing.ZAIProviderProtocol
	})
	if err != nil {
		t.Fatal(err)
	}
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	tier, err := routing.NewTier([]routing.Target{target})
	if err != nil {
		t.Fatal(err)
	}
	routeName, err := routing.ParseRouteName("reasoning-route")
	if err != nil {
		t.Fatal(err)
	}
	route, err := routing.NewRoute(routeName, []routing.Tier{tier})
	if err != nil {
		t.Fatal(err)
	}
	slug, err := routing.ParseWorkspaceSlug("reasoning")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}
