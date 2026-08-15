package openaifamily

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
	"github.com/swobuforge/swobu/internal/wire/messages"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

type commodityCredentialResolver struct{}

func (commodityCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "commodity-token", nil
}

func TestBasetenAndHyperbolicProfilesPreserveTheirDifferentAuthoringContracts(t *testing.T) {
	baseten, ok := profile.ProfileForSpec(string(profile.ProviderSpecBaseten))
	if !ok {
		t.Fatal("Baseten profile is missing")
	}
	if baseten.ProviderDisplayName != "Baseten" || baseten.Locator.Kind != profile.LocatorBaseURL || baseten.Locator.Default != "https://inference.baseten.co/v1" || baseten.ModelDiscovery != profile.ModelDiscoveryModeAdvisory {
		t.Fatalf("Baseten profile = %#v", baseten)
	}
	if got := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecBaseten)); !reflect.DeepEqual(got, []string{"chat_completions_stream", "messages_stream"}) {
		t.Fatalf("Baseten protocols = %v", got)
	}

	hyperbolic, ok := profile.ProfileForSpec(string(profile.ProviderSpecHyperbolic))
	if !ok {
		t.Fatal("Hyperbolic profile is missing")
	}
	if hyperbolic.ProviderDisplayName != "Hyperbolic" || hyperbolic.Locator.Kind != profile.LocatorFixed || hyperbolic.Locator.Default != "https://api.hyperbolic.xyz/v1" || hyperbolic.ModelDiscovery != profile.ModelDiscoveryModeNone {
		t.Fatalf("Hyperbolic profile = %#v", hyperbolic)
	}
	if got := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecHyperbolic)); !reflect.DeepEqual(got, []string{"chat_completions_stream"}) {
		t.Fatalf("Hyperbolic protocols = %v", got)
	}
}

func TestBasetenSharedRuntimeSupportsExactOverrideChatAndMessagesWithoutCatalogSubstitution(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer commodity-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("User-Agent") != "swobu/dev" {
			t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/deployment/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/deployment/v1/chat/completions":
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["model"]) != `"served/exact"` || len(payload["messages"]) == 0 {
				t.Fatalf("Chat payload = %#v", payload)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		case "/deployment/v1/messages":
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["model"]) != `"served/exact"` || len(payload["messages"]) == 0 {
				t.Fatalf("Messages payload = %#v", payload)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"served/exact\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"+
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"+
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\n"+
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	bundle := NewRuntime(server.Client(), commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecBaseten))
	baseURL := server.URL + "/deployment/v1"
	probeTarget := provider.NewTargetSnapshot("baseten", "baseten", baseURL, "env:BASETEN_API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	probe, err := bundle.Discovery.ProbeTarget(context.Background(), probeTarget)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.Options) != 0 {
		t.Fatalf("overridden deployment catalog = %#v, want empty catalog", probe.Options)
	}
	for _, tc := range []struct {
		name     string
		kind     protocolkind.ProtocolKind
		protocol string
	}{
		{name: "chat", kind: protocolkind.ChatCompletions, protocol: "chat_completions"},
		{name: "messages", kind: protocolkind.Messages, protocol: "messages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := provider.NewTargetSnapshot("baseten", "baseten", baseURL, "env:BASETEN_API_KEY", tc.kind, tc.protocol, delivery.BufferedDelivery())
			target.Model = "served/exact"
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Transport.Send(context.Background(), document); err != nil {
				t.Fatal(err)
			}
		})
	}
	if !reflect.DeepEqual(paths, []string{"/deployment/v1/models", "/deployment/v1/chat/completions", "/deployment/v1/messages"}) {
		t.Fatalf("paths = %v", paths)
	}
}

func TestCommodityChatTargetsBridgeResponsesAndMessagesIngress(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolver provider.BackendResolver
	}{
		{name: "baseten", resolver: NewRuntime(nil, commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecBaseten)).BackendResolver},
		{name: "hyperbolic", resolver: NewRuntime(nil, commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecHyperbolic)).BackendResolver},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := provider.NewTargetSnapshot(tc.name, tc.name, "https://example.test/v1", "env:API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
			target.Model = "provider/exact"
			backend, err := tc.resolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			for _, ingress := range []struct {
				name   string
				family protocolkind.ProtocolKind
				decode func(carrier.Document) (wire.ClientDecodeResult, error)
				raw    string
			}{
				{name: "responses", family: protocolkind.Responses, decode: responses.ClientRequestDecoder{}.DecodeClientRequest, raw: `{"model":"provider/exact","input":"hello"}`},
				{name: "messages", family: protocolkind.Messages, decode: messages.ClientRequestDecoder{}.DecodeClientRequest, raw: `{"model":"provider/exact","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`},
			} {
				t.Run(ingress.name, func(t *testing.T) {
					decoded, err := ingress.decode(carrier.NewDocument(ingress.family, "application/json", nil, []byte(ingress.raw), carrier.Meta{}))
					if err != nil {
						t.Fatal(err)
					}
					document, _, err := backend.Codec.Encode(provider.Request{Canonical: decoded.Request.Request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
					if err != nil {
						t.Fatal(err)
					}
					var payload map[string]json.RawMessage
					if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
						t.Fatal(err)
					}
					if string(payload["model"]) != `"provider/exact"` || len(payload["messages"]) == 0 || string(payload["stream"]) != "true" {
						t.Fatalf("%s ingress Chat payload = %#v", ingress.name, payload)
					}
				})
			}
		})
	}
}

func TestHyperbolicManualRuntimeDoesNotProbeOrAddProtocolFamilies(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	bundle := NewRuntime(server.Client(), commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecHyperbolic))
	target := provider.NewTargetSnapshot("hyperbolic", "hyperbolic", server.URL+"/v1", "env:HYPERBOLIC_API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	target.Model = "provider/exact-model"
	result, err := bundle.Discovery.ProbeTarget(context.Background(), target)
	if err == nil || len(result.Options) != 0 || called {
		t.Fatalf("manual discovery = %#v, err=%v, called=%v", result, err, called)
	}
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.Responses, protocolkind.Messages} {
		invalid := target.Clone()
		invalid.ProtocolKind = kind
		if _, err := bundle.BackendResolver.ResolveBackend(invalid); err == nil {
			t.Fatalf("Hyperbolic resolved unsupported protocol %s", kind)
		}
	}
}

func TestCommodityProvidersDispatchUnsupportedOptionalCombination(t *testing.T) {
	dispatched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatched = true
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"selected model rejects this combination"}}`))
	}))
	defer server.Close()
	bundle := NewRuntime(server.Client(), commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecHyperbolic))
	target := provider.NewTargetSnapshot("hyperbolic", "hyperbolic", server.URL+"/v1", "env:HYPERBOLIC_API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	target.Model = "unknown/exact-model"
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), OutputFormat: canonical.Specify(format), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "json")}})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err == nil {
		t.Fatal("provider rejection was not returned")
	}
	if !dispatched {
		t.Fatal("optional combination never reached the provider")
	}
}
