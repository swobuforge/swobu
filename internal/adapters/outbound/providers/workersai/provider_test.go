package workersai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
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
	return "cf-test", nil
}

func TestProfileOwnsAuthoredAccountEndpointManualModelsAndChatResponsesOnly(t *testing.T) {
	manifest, ok := profile.ProfileForSpec(string(profile.ProviderSpecWorkersAI))
	if !ok {
		t.Fatal("Workers AI profile is missing")
	}
	if manifest.ProviderDisplayName != "Cloudflare Workers AI" || manifest.ConnectionShape != routing.ConnectionShapeStandard {
		t.Fatalf("display/shape = %q/%v", manifest.ProviderDisplayName, manifest.ConnectionShape)
	}
	if manifest.Locator.Kind != profile.LocatorBaseURL || manifest.Locator.Default != "" || manifest.Locator.Label != "Workers AI base URL" || !profile.RequiresLocator(string(profile.ProviderSpecWorkersAI)) {
		t.Fatalf("locator = %#v", manifest.Locator)
	}
	if manifest.Credential.Requirement != profile.CredentialRequired || manifest.Credential.SuggestedEnvVar != "CLOUDFLARE_API_TOKEN" {
		t.Fatalf("credential = %#v", manifest.Credential)
	}
	if manifest.ModelDiscovery != profile.ModelDiscoveryModeNone {
		t.Fatalf("catalog = %v", manifest.ModelDiscovery)
	}
	want := []string{"chat_completions_stream", "chat_completions", "responses_stream", "responses"}
	if got := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecWorkersAI)); !reflect.DeepEqual(got, want) {
		t.Fatalf("protocols = %v, want %v", got, want)
	}
	for _, protocol := range manifest.ProviderProtocols {
		if protocol.Kind == protocolkind.Messages {
			t.Fatal("Workers AI must not advertise Messages")
		}
	}
}

func TestChatAndResponsesReuseSharedCodecsWithRequiredGenerationHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cf-test" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("cf-aig-gateway-id") != "default" {
			t.Fatalf("gateway header = %q", r.Header.Get("cf-aig-gateway-id"))
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(payload["model"], []byte("@cf/meta/example")) {
			t.Fatalf("model = %s", payload["model"])
		}
		switch r.URL.Path {
		case "/v1/chat/completions":
			if len(payload["messages"]) == 0 {
				t.Fatalf("shared Chat payload = %#v", payload)
			}
		case "/v1/responses":
			if len(payload["input"]) == 0 {
				t.Fatalf("shared Responses payload = %#v", payload)
			}
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	bundle := NewRuntime(server.Client(), credentialResolver{})
	for _, test := range []struct {
		kind     protocolkind.ProtocolKind
		protocol string
	}{
		{protocolkind.ChatCompletions, "chat_completions"},
		{protocolkind.Responses, "responses"},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			target := workersTarget(server.URL+"/v1", "@cf/meta/example", test.kind, test.protocol)
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			codec, ok := backend.Codec.(protocolcodec.Codec)
			if !ok || codec.Protocol != test.kind {
				t.Fatalf("codec = %#v", backend.Codec)
			}
			if codec.ResponsesDialect.CaptureResponsesContinuation {
				t.Fatal("Workers AI invented provider-native Responses continuation")
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify(target.Model),
				Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
			})
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

func TestModelNamespaceGuardProtectsProviderIdentityWithoutPredictingCapability(t *testing.T) {
	bundle := NewRuntime(nil, credentialResolver{})
	for _, model := range []string{"openai/gpt-5", "anthropic/claude", "google/gemini", "dynamic/customer-support", "@cf/"} {
		t.Run(model, func(t *testing.T) {
			target := workersTarget("https://example.test/v1", model, protocolkind.ChatCompletions, "chat_completions")
			_, err := bundle.BackendResolver.ResolveBackend(target)
			if model == "@cf/" {
				if err != nil {
					t.Fatalf("valid provider namespace rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "@cf/ model identity") || strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("identity error = %v", err)
			}
		})
	}
	for _, protocol := range []struct {
		kind protocolkind.ProtocolKind
		name string
	}{
		{protocolkind.ChatCompletions, "chat_completions"},
		{protocolkind.Responses, "responses"},
	} {
		target := workersTarget("https://example.test/v1", "@cf/future/unknown-model", protocol.kind, protocol.name)
		if _, err := bundle.BackendResolver.ResolveBackend(target); err != nil {
			t.Fatalf("valid Workers AI identity inferred %s capability: %v", protocol.kind, err)
		}
	}
}

func TestManualDiscoveryDoesNotIssueRequestOrReceiveGenerationHeader(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), workersTarget(server.URL, "@cf/model", protocolkind.ChatCompletions, "chat_completions"))
	if err == nil || len(result.Options) != 0 {
		t.Fatalf("manual discovery = %#v, %v", result, err)
	}
	if called {
		t.Fatal("manual Workers AI discovery issued a network request")
	}
}

func TestUnsupportedValidWorkersAICombinationReachesCloudflare(t *testing.T) {
	dispatched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatched = true
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"this Workers AI model does not expose Responses"}}`))
	}))
	defer server.Close()

	target := workersTarget(server.URL+"/v1", "@cf/vendor/future-model", protocolkind.Responses, "responses")
	backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatalf("model/protocol capability was inferred locally: %v", err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err == nil {
		t.Fatal("Cloudflare rejection was not returned")
	}
	if !dispatched {
		t.Fatal("valid Workers AI target never reached Cloudflare")
	}
}

func TestSharedEncodingHasNoUserAgentDialectInput(t *testing.T) {
	target := workersTarget("https://example.test/v1", "@cf/meta/example", protocolkind.ChatCompletions, "chat_completions")
	backend, err := NewRuntime(nil, credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
	first, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := backend.Codec.Encode(provider.Request{Canonical: request.Clone(), Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.RawBytes(), second.RawBytes()) {
		t.Fatalf("identical semantic requests encoded differently:\n%s\n%s", first.RawBytes(), second.RawBytes())
	}
}

func workersTarget(baseURL, model string, kind protocolkind.ProtocolKind, protocol string) provider.TargetSnapshot {
	providerProtocol, ok := profile.ProviderProtocolSpecForSpec(string(profile.ProviderSpecWorkersAI), protocol)
	if !ok {
		panic("Workers AI test target uses an unknown concrete protocol")
	}
	target := provider.NewTargetSnapshot("workersai", string(profile.ProviderSpecWorkersAI), baseURL, "env:CLOUDFLARE_API_TOKEN", kind, protocol, providerProtocol.Delivery)
	target.Model = model
	return target
}
