package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

// TestFriendliStrictBackendReceivesOnlyAdmittedChatSemantics is the Cline
// regression in its architectural form. The inbound Chat decoder tolerates an
// extension but never canonicalizes it, so the exact same Friendli request is
// produced regardless of the client identity that supplied it.
func TestFriendliStrictBackendReceivesOnlyAdmittedChatSemantics(t *testing.T) {
	var captured []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("Friendli path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode Friendli request: %v", err)
		}
		captured = append(captured, request)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_friendli\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"zai-org/GLM-5.2\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_friendli\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"zai-org/GLM-5.2\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	registry := mustProviderRegistry(t, rewritingClientForServer(t, upstream), testCredentialResolver{})
	runtime := reasoningRequestPathRuntime{RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(), ProviderRegistry: registry}
	ingress := exchange.NewIngress(nil, runtime, exchange.RuntimePoliciesSpec{})
	workspace := friendliWorkspace(t)
	body := []byte(`{"model":"friendli-route","messages":[{"role":"user","content":"Return one sentence."}],"reasoning":{"exclude":true},"stream":true}`)

	for _, userAgent := range []string{"Cline/3.0", "another-client/1.0"} {
		header := http.Header{"User-Agent": []string{userAgent}}
		out, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, exchange.RequestInput{
			ExchangeID:      "friendli-extension-" + userAgent,
			Request:         exchange.NewTransportRequest(http.MethodPost, "/chat/completions", header, body),
			ClientFamily:    canonical.ClientFamilyChatCompletions,
			ResponseFraming: delivery.FramingSSE,
		})
		if err != nil {
			t.Fatalf("HandleRequestWithWorkspace(%q): %v", userAgent, err)
		}
		response, ok := out.Response.(exchange.StreamingResponse)
		if !ok {
			t.Fatalf("response = %T, want streaming Chat response", out.Response)
		}
		if _, err := io.ReadAll(response.Response.Body); err != nil {
			t.Fatalf("consume Chat response: %v", err)
		}
	}

	if len(captured) != 2 {
		t.Fatalf("Friendli request count = %d, want 2", len(captured))
	}
	if !reflect.DeepEqual(captured[0], captured[1]) {
		t.Fatalf("User-Agent changed Friendli request:\nfirst=%#v\nsecond=%#v", captured[0], captured[1])
	}
	request := captured[0]
	if _, present := request["reasoning"]; present {
		t.Fatalf("unadmitted client extension leaked to Friendli: %#v", request)
	}
	if _, present := request["parse_reasoning"]; present {
		t.Fatalf("ignored client extension created Friendli disclosure projection: %#v", request)
	}
	if request["model"] != "zai-org/GLM-5.2" || request["stream"] != true {
		t.Fatalf("ordinary Chat semantics changed: %#v", request)
	}
}

func friendliWorkspace(t *testing.T) routing.Workspace {
	t.Helper()
	provider, _ := routing.ParseProvider("friendli", profile.SupportsSpec)
	connection, err := routing.NewStandardConnection(provider, "https://friendli-gateway.example/v1", "env:FRIENDLI_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := routing.ParseTargetID("friendli-target")
	if err != nil {
		t.Fatal(err)
	}
	model, err := routing.ParseUpstreamModel("zai-org/GLM-5.2")
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := routing.ParseProtocol("chat_completions_stream", provider, profile.RoutingConstructionFacts().ProtocolSupported)
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
	routeName, err := routing.ParseRouteName("friendli-route")
	if err != nil {
		t.Fatal(err)
	}
	route, err := routing.NewRoute(routeName, []routing.Tier{tier})
	if err != nil {
		t.Fatal(err)
	}
	slug, err := routing.ParseWorkspaceSlug("friendli")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}
