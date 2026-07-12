package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	exchangeruntime "github.com/swobuforge/swobu/internal/adapters/wire/exchangeruntime"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
)

type testCredentialResolver struct{}

func (testCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "token_test", nil
}

func mustProviderRequestWithDocument(t *testing.T, request canonical.CanonicalRequest, contract exchange.ExecutionContract, target exchange.RoutableTarget) ports.ProviderRequest {
	t.Helper()
	codec := exchangeruntime.NewResolver().ProviderRequestDocumentEncoder(target.ProtocolKind)
	if codec == nil {
		t.Fatalf("provider request encoder missing for protocol %s", target.ProtocolKind)
	}
	wireRequest, err := codec.EncodeProviderRequestDocument(request, contract.ProviderDelivery)
	if err != nil {
		t.Fatalf("encode provider request document: %v", err)
	}
	return ports.NewProviderRequest(request, wireRequest, contract, target)
}

func TestServices_ExecutionDispatchesByProviderID(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		case "/v1/messages":
			_, _ = w.Write([]byte(`{"id":"msg_1","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected path"}`))
		}
	}))
	defer upstream.Close()

	composition := NewProviderIngressResolverComposition(upstream.Client(), testCredentialResolver{}, "")

	openAIReq := mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "chat_completions"),
	)
	if _, err := composition.ResolveProviderIngress(context.Background(), openAIReq); err != nil {
		t.Fatalf("openai execution failed: %v", err)
	}

	anthropicReq := mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-b", "anthropic", upstream.URL+"/v1", "cred-1", protocolkind.Messages, "credential_ref", "", "messages"),
	)
	if _, err := composition.ResolveProviderIngress(context.Background(), anthropicReq); err != nil {
		t.Fatalf("anthropic execution failed: %v", err)
	}
}

func TestServices_ModelCatalogDispatchesByProviderID(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	defer upstream.Close()

	composition := NewProviderIngressResolverComposition(upstream.Client(), testCredentialResolver{}, "")

	openAIModels, err := composition.ListModels(context.Background(), exchange.NewRoutableTarget(
		"backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "",
	))
	if err != nil {
		t.Fatalf("openai model catalog failed: %v", err)
	}
	if len(openAIModels) != 2 {
		t.Fatalf("openai model catalog len=%d want 2", len(openAIModels))
	}

	_, err = composition.ListModels(context.Background(), exchange.NewRoutableTarget(
		"backend-b", "chatgpt", upstream.URL+"/v1", "keychain:chatgpt/default", protocolkind.ChatCompletions, "credential_ref", "", "",
	))
	if err == nil || !strings.Contains(err.Error(), "subscription tier") {
		t.Fatalf("chatgpt catalog dispatch must use chatgpt adapter tier validation, got err=%v", err)
	}
}

func TestServices_UnknownProviderIDFailsFast(t *testing.T) {
	t.Parallel()

	composition := NewProviderIngressResolverComposition(http.DefaultClient, testCredentialResolver{}, "")
	_, err := composition.ResolveProviderIngress(context.Background(), mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:     "m",
			InputText: "hi",
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-a", "unknown-provider", "https://example.test/v1", "cred-1", protocolkind.Completions, "credential_ref", "", ""),
	))
	if err == nil || !strings.Contains(err.Error(), "provider id is unsupported") {
		t.Fatalf("unknown provider must fail fast, got err=%v", err)
	}
}

func TestServices_ValidateCredentialsDispatchesByProviderID(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"}]}`))
	}))
	defer upstream.Close()

	composition := NewProviderIngressResolverComposition(upstream.Client(), testCredentialResolver{}, "")
	err := composition.ValidateCredentials(context.Background(), exchange.NewRoutableTarget(
		"backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "",
	))
	if err != nil {
		t.Fatalf("openai validate credentials failed: %v", err)
	}
}

func TestServices_OpenAIFamilyCacheRetentionDegradation_IsProviderDeterministic(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	composition := NewProviderIngressResolverComposition(upstream.Client(), testCredentialResolver{}, "")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		CacheIntent: canonical.NewCacheIntent(canonical.CacheIntentParams{
			Key:       "repo-alpha",
			Retention: canonical.CacheRetention24H,
		}),
	})

	openAIResp, err := composition.ResolveProviderIngress(context.Background(), mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-openai", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "chat_completions"),
	))
	if err != nil {
		t.Fatalf("openai execution failed: %v", err)
	}
	if err := exchange.ValidateProviderIngress(openAIResp); err != nil {
		t.Fatalf("openai ingress invalid: %v", err)
	}

	ollamaResp, err := composition.ResolveProviderIngress(context.Background(), mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-ollama", "ollama", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "chat_completions"),
	))
	if err != nil {
		t.Fatalf("ollama execution failed: %v", err)
	}
	if err := exchange.ValidateProviderIngress(ollamaResp); err != nil {
		t.Fatalf("ollama ingress invalid: %v", err)
	}
}
