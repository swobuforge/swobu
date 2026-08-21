package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestNVIDIAHostedProfileIsFixedEnumerableDerivedChat(t *testing.T) {
	manifest, ok := profile.ProfileForSpec(string(profile.ProviderSpecNVIDIA))
	if !ok {
		t.Fatal("NVIDIA hosted profile is missing")
	}
	if manifest.ProviderDisplayName != "NVIDIA NIM Hosted" || manifest.ConnectionShape != routing.ConnectionShapeStandard {
		t.Fatalf("display/shape = %q/%v", manifest.ProviderDisplayName, manifest.ConnectionShape)
	}
	if manifest.Locator.Kind != profile.LocatorFixed || manifest.Locator.Default != "https://integrate.api.nvidia.com/v1" {
		t.Fatalf("locator = %#v", manifest.Locator)
	}
	if manifest.Credential.Requirement != profile.CredentialRequired || manifest.Credential.SuggestedEnvVar != "NVIDIA_API_KEY" {
		t.Fatalf("credential = %#v", manifest.Credential)
	}
	if manifest.ModelDiscovery != profile.ModelDiscoveryModeAdvisory {
		t.Fatalf("catalog = %v", manifest.ModelDiscovery)
	}
	protocol, derived := profile.DerivedProtocolForSpec(string(profile.ProviderSpecNVIDIA))
	if !derived || protocol != "chat_completions_stream" || !reflect.DeepEqual(profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecNVIDIA)), []string{"chat_completions_stream"}) {
		t.Fatalf("derived protocol = %q, %v", protocol, derived)
	}
}

func TestNVIDIAHostedUsesGenericCatalogAndUnmodifiedStreamingChat(t *testing.T) {
	const model = "publisher/future-model"
	var catalogCalls, chatCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token_test" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/models":
			catalogCalls++
			_, _ = w.Write([]byte(`{"data":[{"id":"catalog/model"}]}`))
		case "/v1/chat/completions":
			chatCalls++
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["model"]) != `"`+model+`"` || string(payload["stream"]) != "true" {
				t.Fatalf("model/stream = %s/%s", payload["model"], payload["stream"])
			}
			for _, field := range []string{"messages", "tools", "response_format"} {
				if len(payload[field]) == 0 {
					t.Fatalf("shared Chat field %q missing: %#v", field, payload)
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	registry := mustProviderRegistry(t, server.Client(), testCredentialResolver{})
	target := nvidiaTarget(server.URL+"/v1", model)
	result, err := registry.ProbeTarget(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Options) != 1 || result.Options[0].Name != "catalog/model" {
		t.Fatalf("catalog = %#v", result.Options)
	}
	backend, err := registry.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := backend.Codec.(protocolcodec.Codec)
	if !ok || codec.Protocol != protocolkind.ChatCompletions || codec.ResponsesDialect.CaptureResponsesContinuation {
		t.Fatalf("NVIDIA codec = %#v", backend.Codec)
	}
	request := canonicaltest.LargeIntegerRequest(t, model)
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(document.RawBytes(), []byte("9007199254740993")); got != 3 {
		t.Fatalf("shared JSON semantics changed: %s", document.RawBytes())
	}
	if _, err := backend.Transport.Send(context.Background(), document); err != nil {
		t.Fatal(err)
	}
	if catalogCalls != 1 || chatCalls != 1 {
		t.Fatalf("catalog/chat calls = %d/%d", catalogCalls, chatCalls)
	}
}

func TestNVIDIAHostedDoesNotPreflightHeterogeneousModelCapabilities(t *testing.T) {
	dispatched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatched = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"selected model rejects this field"}}`))
	}))
	defer server.Close()

	registry := mustProviderRegistry(t, server.Client(), testCredentialResolver{})
	target := nvidiaTarget(server.URL, "any-publisher/any-model")
	backend, err := registry.ResolveBackend(target)
	if err != nil {
		t.Fatalf("NVIDIA model capability was inferred locally: %v", err)
	}
	effort := canonical.InferenceEffortXHigh
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(target.Model), Controls: controls,
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
	})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document.RawBytes(), []byte(`"reasoning_effort":"xhigh"`)) {
		t.Fatalf("shared reasoning field was removed: %s", document.RawBytes())
	}
	if _, err := backend.Transport.Send(context.Background(), document); err == nil {
		t.Fatal("NVIDIA backend rejection was not returned")
	}
	if !dispatched {
		t.Fatal("request never reached NVIDIA")
	}
}

func TestNVIDIAHostedResponsesAndMessagesIngressLowerToSharedStreamingChat(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("NVIDIA provider path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode NVIDIA request: %v", err)
		}
		requests = append(requests, request)
		if request["model"] != "publisher/model" || request["stream"] != true {
			t.Fatalf("NVIDIA model/stream = %#v/%#v", request["model"], request["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_nvidia\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"publisher/model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_nvidia\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"publisher/model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	registry := mustProviderRegistry(t, rewritingClientForServer(t, server), testCredentialResolver{})
	runtime := nvidiaRequestPathRuntime{
		RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(),
		ProviderRegistry:     registry,
	}
	ingress := exchange.NewIngress(nil, runtime, exchange.RuntimePoliciesSpec{})
	workspace := nvidiaWorkspace(t)

	cases := []struct {
		name          string
		family        canonical.ClientFamily
		path          string
		body          string
		expectedField string
	}{
		{
			name:          "responses",
			family:        canonical.ClientFamilyResponses,
			path:          "/responses",
			body:          `{"model":"nvidia-route","input":"hello","stream":true}`,
			expectedField: "messages",
		},
		{
			name:          "messages",
			family:        canonical.ClientFamilyMessages,
			path:          "/messages",
			body:          `{"model":"nvidia-route","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":true}`,
			expectedField: "messages",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, exchange.RequestInput{
				ExchangeID:      "nvidia-" + tc.name,
				Request:         exchange.NewTransportRequest(http.MethodPost, tc.path, nil, []byte(tc.body)),
				ClientFamily:    tc.family,
				ResponseFraming: delivery.FramingSSE,
			})
			if err != nil {
				t.Fatalf("NVIDIA %s ingress: %v", tc.name, err)
			}
			consumeReasoningResponse(t, out.Response)
			if len(requests) != 1 {
				t.Fatalf("NVIDIA %s provider requests = %d, want one", tc.name, len(requests))
			}
			request := requests[0]
			if _, ok := request[tc.expectedField]; !ok {
				t.Fatalf("NVIDIA %s lowering omitted shared Chat %q: %#v", tc.name, tc.expectedField, request)
			}
			if _, ok := request["input"]; ok {
				t.Fatalf("NVIDIA %s request retained client-family input field: %#v", tc.name, request)
			}
			requests = requests[:0]
		})
	}
}

func TestNVIDIAHostedClientIdentityDoesNotChangeProviderRequest(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("NVIDIA provider path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode NVIDIA request: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_nvidia_ua\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"publisher/model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	registry := mustProviderRegistry(t, rewritingClientForServer(t, server), testCredentialResolver{})
	runtime := nvidiaRequestPathRuntime{
		RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(),
		ProviderRegistry:     registry,
	}
	ingress := exchange.NewIngress(nil, runtime, exchange.RuntimePoliciesSpec{})
	workspace := nvidiaWorkspace(t)
	for _, userAgent := range []string{"ResponsesClient/1.0", "MessagesClient/1.0", ""} {
		header := http.Header{}
		if userAgent != "" {
			header.Set("User-Agent", userAgent)
		}
		out, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, exchange.RequestInput{
			ExchangeID:      "nvidia-client-" + userAgent,
			Request:         exchange.NewTransportRequest(http.MethodPost, "/chat/completions", header, []byte(`{"model":"nvidia-route","messages":[{"role":"user","content":"hello"}],"stream":true}`)),
			ClientFamily:    canonical.ClientFamilyChatCompletions,
			ResponseFraming: delivery.FramingSSE,
		})
		if err != nil {
			t.Fatalf("NVIDIA request for User-Agent %q: %v", userAgent, err)
		}
		consumeReasoningResponse(t, out.Response)
	}
	if len(requests) != 3 {
		t.Fatalf("NVIDIA provider requests = %d, want 3", len(requests))
	}
	if !reflect.DeepEqual(requests[0], requests[1]) || !reflect.DeepEqual(requests[1], requests[2]) {
		t.Fatalf("client identity changed NVIDIA request:\nfirst=%#v\nsecond=%#v\nthird=%#v", requests[0], requests[1], requests[2])
	}
}

type nvidiaRequestPathRuntime struct {
	codecresolver.RuntimeCodecResolver
	ProviderRegistry
}

func nvidiaWorkspace(t *testing.T) routing.Workspace {
	t.Helper()
	providerID, err := routing.ParseProvider(string(profile.ProviderSpecNVIDIA), profile.SupportsSpec)
	if err != nil {
		t.Fatalf("NVIDIA provider did not parse: %v", err)
	}
	connection, err := routing.NewStandardConnection(providerID, "", "env:NVIDIA_API_KEY")
	if err != nil {
		t.Fatalf("NVIDIA StandardConnection: %v", err)
	}
	targetID, err := routing.ParseTargetID("nvidia-target")
	if err != nil {
		t.Fatal(err)
	}
	model, err := routing.ParseUpstreamModel("publisher/model")
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := routing.ParseProtocol("chat_completions_stream", providerID, profile.RoutingConstructionFacts().ProtocolSupported)
	if err != nil {
		t.Fatalf("NVIDIA derived protocol: %v", err)
	}
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	tier, err := routing.NewTier([]routing.Target{target})
	if err != nil {
		t.Fatal(err)
	}
	routeName, err := routing.ParseRouteName("nvidia-route")
	if err != nil {
		t.Fatal(err)
	}
	route, err := routing.NewRoute(routeName, []routing.Tier{tier})
	if err != nil {
		t.Fatal(err)
	}
	slug, err := routing.ParseWorkspaceSlug("nvidia")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func nvidiaTarget(baseURL, model string) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("nvidia", string(profile.ProviderSpecNVIDIA), baseURL, "env:NVIDIA_API_KEY", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = model
	return target
}
