package llm7

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
	return "llm7-token", nil
}

func TestProfileOwnsFixedOptionalCredentialAndDerivedChat(t *testing.T) {
	manifest, ok := profile.ProfileForSpec(string(profile.ProviderSpecLLM7))
	if !ok {
		t.Fatal("LLM7 profile is missing")
	}
	if manifest.ProviderDisplayName != "LLM7" || manifest.ConnectionShape != routing.ConnectionShapeStandard {
		t.Fatalf("display/shape = %q/%v", manifest.ProviderDisplayName, manifest.ConnectionShape)
	}
	if manifest.Locator.Kind != profile.LocatorFixed || manifest.Locator.Default != "https://api.llm7.io/v1" {
		t.Fatalf("locator = %#v", manifest.Locator)
	}
	if manifest.Credential.Requirement != profile.CredentialOptional || manifest.Credential.SuggestedEnvVar != "LLM7_API_KEY" {
		t.Fatalf("credential = %#v", manifest.Credential)
	}
	if manifest.ModelDiscovery != profile.ModelDiscoveryModeAdvisory {
		t.Fatalf("catalog = %v", manifest.ModelDiscovery)
	}
	protocol, derived := profile.DerivedProtocolForSpec(string(profile.ProviderSpecLLM7))
	if !derived || protocol != "chat_completions_stream" || !reflect.DeepEqual(profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecLLM7)), []string{"chat_completions_stream"}) {
		t.Fatalf("derived protocol = %q, %v", protocol, derived)
	}
}

func TestRuntimeOptionalCredentialAndSharedUserAgent(t *testing.T) {
	for _, test := range []struct {
		name          string
		credentialRef string
		wantAuth      string
	}{
		{name: "anonymous"},
		{name: "configured token", credentialRef: "env:LLM7_API_KEY", wantAuth: "Bearer llm7-token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
					t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != test.wantAuth {
					t.Fatalf("Authorization = %q, want %q", got, test.wantAuth)
				}
				if got := r.Header.Get("User-Agent"); got != "swobu/dev" {
					t.Fatalf("User-Agent = %q, want shared runtime value", got)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
			}))
			defer server.Close()

			target := llm7Target(server.URL+"/v1", test.credentialRef, "default")
			backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify(target.Model),
				Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
			})
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Transport.Send(context.Background(), document); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDiscoveryDecodesTopLevelArrayWithSelectorsFirstAndUniqueConcreteIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("anonymous discovery sent Authorization: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`[
            {"id":"future-z","owned_by":"provider-z","tools_calling":true},
            {"id":"fast","owned_by":"provider-fast"},
            {"id":"future-a","owned_by":"provider-a","tools_calling":false},
            {"id":"future-z","owned_by":"duplicate"},
            {"id":"","owned_by":"ignored"}
        ]`))
	}))
	defer server.Close()

	result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), llm7Target(server.URL+"/v1", "", "default"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, deployment := range result.Options {
		got = append(got, deployment.Name)
	}
	want := []string{"default", "fast", "pro", "future-a", "future-z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployments = %v, want %v", got, want)
	}
	if strings.Contains(strings.Join(got, "\n"), "provider-") {
		t.Fatalf("catalog metadata leaked into deployment identity: %v", got)
	}
}

func TestDiscoveryConfiguredCredentialUsesBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer llm7-token" {
			t.Fatalf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`[{"id":"token-model"}]`))
	}))
	defer server.Close()

	result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), llm7Target(server.URL+"/v1", "env:LLM7_API_KEY", "token-model"))
	if err != nil || len(result.Options) != 4 || result.Options[3].Name != "token-model" {
		t.Fatalf("deployments/error = %#v / %v", result.Options, err)
	}
}

func TestDiscoveryDecodesObservedOpenAIStyleEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer llm7-token" {
			t.Fatalf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"DeepSeek-V4-Flash-0731","tools_calling":true,"tier":"turbo"}]}`))
	}))
	defer server.Close()

	result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), llm7Target(server.URL+"/v1", "env:LLM7_API_KEY", "default"))
	if err != nil || len(result.Options) != 4 || result.Options[3].Name != "DeepSeek-V4-Flash-0731" {
		t.Fatalf("deployments/error = %#v / %v", result.Options, err)
	}
}

func TestSelectorRemainsExactAfterConcreteResponseModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "fast" {
			t.Fatalf("request model = %q, want selector fast", payload.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"future-concrete-model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	target := llm7Target(server.URL+"/v1", "", "fast")
	backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("fast"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err != nil {
		t.Fatal(err)
	}
	if backend.Target.Model != "fast" || target.Model != "fast" {
		t.Fatalf("selector mutated after provider response: backend=%q target=%q", backend.Target.Model, target.Model)
	}
}

func TestOptionalChatSemanticsReachLLM7WithoutPlanPreflight(t *testing.T) {
	dispatched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatched = true
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"messages", "tools", "response_format"} {
			if len(payload[field]) == 0 {
				t.Fatalf("shared Chat field %q missing: %#v", field, payload)
			}
		}
		if !bytesContains(payload["messages"], "image_url") {
			t.Fatalf("vision content missing: %s", payload["messages"])
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"selected plan does not allow this feature"}}`))
	}))
	defer server.Close()

	request, names := llm7OptionalRequest(t)
	target := llm7Target(server.URL+"/v1", "", "default")
	backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatalf("local plan/capability preflight rejected request: %v", err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err == nil {
		t.Fatal("provider rejection was not returned")
	}
	if !dispatched {
		t.Fatal("optional tool/JSON/vision request never reached LLM7")
	}
}

func llm7Target(baseURL, credential, model string) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("llm7", string(profile.ProviderSpecLLM7), baseURL, credential, protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	target.Model = model
	return target
}

func llm7OptionalRequest(t *testing.T) (canonical.CanonicalRequest, provider.AttemptToolNames) {
	t.Helper()
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	tool := canonicaltest.MustFunctionTool(key, "lookup", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	image, err := canonical.NewURLImage("https://example.test/image.png", canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewTextMessagePart("describe this image"),
		canonical.NewImageMessagePart(image),
	})
	if err != nil {
		t.Fatal(err)
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        canonical.Specify("default"),
		Items:        []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tool), message},
		OutputFormat: canonical.Specify(format),
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	return request, names
}

func bytesContains(raw []byte, want string) bool {
	return strings.Contains(string(raw), want)
}

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery
