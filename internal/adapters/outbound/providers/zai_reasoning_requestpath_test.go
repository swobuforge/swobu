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
	connection, err := routing.NewZAIConnection(routing.ZAIAccessGeneralAPI, "env:ZAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := routing.ParseProtocol(routing.ZAIProviderProtocol, routing.ProviderZAI, func(provider routing.Provider, candidate string) bool {
		return provider == routing.ProviderZAI && candidate == routing.ZAIProviderProtocol
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
