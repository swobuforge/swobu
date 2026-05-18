package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ports"
)

type testCredentialResolver struct{}

func (testCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "token_test", nil
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

	services := NewProviderServicesBundle(upstream.Client(), testCredentialResolver{})

	openAIReq := ports.NewProviderRequest(
		canonical.NewDialogRequest("m", []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")}),
		ports.NewExecutionContract(false),
		ports.NewRoutableTarget("backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref"),
	)
	if _, err := services.Execution.Execute(context.Background(), openAIReq); err != nil {
		t.Fatalf("openai execution failed: %v", err)
	}

	anthropicReq := ports.NewProviderRequest(
		canonical.NewDialogRequest("m", []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")}),
		ports.NewExecutionContract(false),
		ports.NewRoutableTarget("backend-b", "anthropic", upstream.URL+"/v1", "cred-1", protocolkind.Messages, "credential_ref"),
	)
	if _, err := services.Execution.Execute(context.Background(), anthropicReq); err != nil {
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

	services := NewProviderServicesBundle(upstream.Client(), testCredentialResolver{})

	openAIModels, err := services.ModelCatalog.ListModels(context.Background(), ports.NewRoutableTarget(
		"backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref",
	))
	if err != nil {
		t.Fatalf("openai model catalog failed: %v", err)
	}
	if len(openAIModels) != 2 {
		t.Fatalf("openai model catalog len=%d want 2", len(openAIModels))
	}

	_, err = services.ModelCatalog.ListModels(context.Background(), ports.NewRoutableTarget(
		"backend-b", "chatgpt", upstream.URL+"/v1", "keychain:chatgpt/default", protocolkind.ChatCompletions, "credential_ref",
	))
	if err == nil || !strings.Contains(err.Error(), "subscription tier") {
		t.Fatalf("chatgpt catalog dispatch must use chatgpt adapter tier validation, got err=%v", err)
	}
}

func TestServices_UnknownProviderIDFailsFast(t *testing.T) {
	t.Parallel()

	services := NewProviderServicesBundle(http.DefaultClient, testCredentialResolver{})
	_, err := services.Execution.Execute(context.Background(), ports.NewProviderRequest(
		canonical.NewPromptRequest("m", "hi"),
		ports.NewExecutionContract(false),
		ports.NewRoutableTarget("backend-a", "unknown-provider", "https://example.test/v1", "cred-1", protocolkind.Completions, "credential_ref"),
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

	services := NewProviderServicesBundle(upstream.Client(), testCredentialResolver{})
	err := services.ModelCatalog.ValidateCredentials(context.Background(), ports.NewRoutableTarget(
		"backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref",
	))
	if err != nil {
		t.Fatalf("openai validate credentials failed: %v", err)
	}
}
