package siliconflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "siliconflow-key", nil
}

func TestProfileOwnsFixedBearerAuthoredChatAndMessages(t *testing.T) {
	manifest, ok := profile.ProfileForSpec(string(profile.ProviderSpecSiliconFlow))
	if !ok {
		t.Fatal("SiliconFlow profile is missing")
	}
	if manifest.ProviderDisplayName != "SiliconFlow" || manifest.ConnectionShape != routing.ConnectionShapeStandard {
		t.Fatalf("display/shape = %q/%v", manifest.ProviderDisplayName, manifest.ConnectionShape)
	}
	if manifest.Locator.Kind != profile.LocatorFixed || manifest.Locator.Default != "https://api.siliconflow.cn/v1" {
		t.Fatalf("locator = %#v", manifest.Locator)
	}
	if manifest.Credential.Requirement != profile.CredentialRequired || manifest.Credential.SuggestedEnvVar != "SILICONFLOW_API_KEY" {
		t.Fatalf("credential = %#v", manifest.Credential)
	}
	if manifest.ModelDiscovery != profile.ModelDiscoveryModeAdvisory {
		t.Fatalf("catalog = %v", manifest.ModelDiscovery)
	}
	if got := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecSiliconFlow)); !reflect.DeepEqual(got, []string{"chat_completions_stream", "messages_stream"}) {
		t.Fatalf("protocols = %v", got)
	}
	if profile.SupportsProviderProtocolForSpec(string(profile.ProviderSpecSiliconFlow), "responses") {
		t.Fatal("SiliconFlow must not advertise Responses")
	}
}

func TestDiscoveryUsesFilteredTextChatQueryAndSharedModelDecoder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("type") != "text" || r.URL.Query().Get("sub_type") != "chat" {
			t.Fatalf("query = %v", r.URL.Query())
		}
		if r.Header.Get("Authorization") != "Bearer siliconflow-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("User-Agent") != "swobu/dev" {
			t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"Pro/model"},{"id":"image/model"},{"id":"Pro/model"},{"id":""}]}`))
	}))
	defer server.Close()

	target := siliconFlowTarget(server.URL+"/v1", protocolkind.ChatCompletions, "chat_completions_stream")
	result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Options) != 2 || result.Options[0].Name != "Pro/model" || result.Options[1].Name != "image/model" {
		t.Fatalf("deployments = %#v", result.Options)
	}
}

func TestResponsesIngressReachesSiliconFlowChatAndMessagesIngressReachesNativeMessages(t *testing.T) {
	bundle := NewRuntime(nil, credentialResolver{})
	for _, tc := range []struct {
		name  string
		kind  protocolkind.ProtocolKind
		proto string
	}{
		{name: "chat", kind: protocolkind.ChatCompletions, proto: "chat_completions_stream"},
		{name: "messages", kind: protocolkind.Messages, proto: "messages_stream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := siliconFlowTarget("https://example.test/v1", tc.kind, tc.proto)
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			req := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("unlisted/exact-model"),
				Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
			})
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: req, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["model"]) != `"unlisted/exact-model"` || len(payload["messages"]) == 0 || string(payload["stream"]) != "true" {
				t.Fatalf("%s provider payload = %#v", tc.name, payload)
			}
		})
	}
}

func TestChatAndMessagesUseSharedBearerRuntimeAndExactModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer siliconflow-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if string(payload["model"]) != `"unlisted/exact-model"` {
			t.Fatalf("model = %s", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			if len(payload["messages"]) == 0 {
				t.Fatalf("Chat payload = %#v", payload)
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		case "/v1/messages":
			if len(payload["messages"]) == 0 {
				t.Fatalf("Messages payload = %#v", payload)
			}
			_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"unlisted/exact-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	bundle := NewRuntime(server.Client(), credentialResolver{})
	for _, tc := range []struct {
		name     string
		kind     protocolkind.ProtocolKind
		protocol string
	}{
		{name: "chat", kind: protocolkind.ChatCompletions, protocol: "chat_completions_stream"},
		{name: "messages", kind: protocolkind.Messages, protocol: "messages_stream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := siliconFlowTarget(server.URL+"/v1", tc.kind, tc.protocol)
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Transport.Send(context.Background(), document); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func siliconFlowTarget(baseURL string, kind protocolkind.ProtocolKind, protocol string) provider.TargetSnapshot {
	providerProtocol, ok := profile.ProviderProtocolSpecForSpec(string(profile.ProviderSpecSiliconFlow), protocol)
	if !ok {
		panic("SiliconFlow test target uses an unknown concrete protocol")
	}
	target := provider.NewTargetSnapshot("siliconflow", string(profile.ProviderSpecSiliconFlow), baseURL, "env:SILICONFLOW_API_KEY", kind, protocol, providerProtocol.Delivery)
	target.Model = "unlisted/exact-model"
	return target
}

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery
