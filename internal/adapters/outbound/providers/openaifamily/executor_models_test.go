package openaifamily

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

type stubCredentialResolver struct{}

func (stubCredentialResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	return "token_test", nil
}

func TestListModels_NonChatGPTMissingModelReadScopeDoesNotFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"Missing scopes: api.model.read"}`))
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{}, NewOpenRouterPolicy())
	_, err := exec.ListModels(context.Background(), exchange.NewRoutableTarget(
		"draft",
		"openrouter",
		srv.URL+"/v1",
		"env:OPENROUTER_API_KEY",
		protocolkind.ChatCompletions,
		"credential_ref",
		"",
		"",
	))
	if err == nil {
		t.Fatal("expected backend error for non-chatgpt provider")
	}
}

func TestListModels_OpenAIRequiresCredentialRef(t *testing.T) {
	t.Parallel()

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{}, NewOpenAIPolicy())
	_, err := exec.ListModels(context.Background(), exchange.NewRoutableTarget(
		"draft",
		"openai",
		srv.URL+"/v1",
		"",
		protocolkind.ChatCompletions,
		"credential_ref",
		"",
		"",
	))
	if err == nil {
		t.Fatal("expected missing credential ref error")
	}
	var swErr canonical.Error
	if !errors.As(err, &swErr) || swErr.Code != canonical.ErrorCodeBadEndpoint {
		t.Fatalf("error = %v, want BAD_ENDPOINT", err)
	}
	if hits != 0 {
		t.Fatalf("upstream hits = %d, want 0", hits)
	}
}

func TestListModels_OpenRouterRequiresCredentialRef(t *testing.T) {
	t.Parallel()

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{}, NewOpenRouterPolicy())
	_, err := exec.ListModels(context.Background(), exchange.NewRoutableTarget(
		"draft",
		"openrouter",
		srv.URL+"/v1",
		"",
		protocolkind.ChatCompletions,
		"credential_ref",
		"",
		"",
	))
	if err == nil {
		t.Fatal("expected missing credential ref error")
	}
	var swErr canonical.Error
	if !errors.As(err, &swErr) || swErr.Code != canonical.ErrorCodeBadEndpoint {
		t.Fatalf("error = %v, want BAD_ENDPOINT", err)
	}
	if hits != 0 {
		t.Fatalf("upstream hits = %d, want 0", hits)
	}
}

func TestListModels_OpenAICompatibleUsesSelectedAuthHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		authHeader string
		wantHeader string
		wantValue  string
	}{
		{name: "default authorization", authHeader: "", wantHeader: "Authorization", wantValue: "Bearer token_test"},
		{name: "api key", authHeader: "api-key", wantHeader: "api-key", wantValue: "token_test"},
		{name: "custom", authHeader: "X-Custom-Auth", wantHeader: "X-Custom-Auth", wantValue: "token_test"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
					t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
				}
				if got := r.Header.Get(tt.wantHeader); got != tt.wantValue {
					t.Fatalf("header %s=%q want %q", tt.wantHeader, got, tt.wantValue)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-4.1-mini"}]}`))
			}))
			defer srv.Close()

			exec := NewExecutor(srv.Client(), stubCredentialResolver{}, NewOpenAICompatiblePolicy())
			target := exchange.NewRoutableTarget(
				"draft",
				"openai_compatible",
				srv.URL+"/v1",
				"env:OPENAI_API_KEY",
				protocolkind.ChatCompletions,
				"credential_ref",
				"",
				"",
			)
			target.AuthHeader = tt.authHeader
			models, err := exec.ListModels(context.Background(), target)
			if err != nil {
				t.Fatalf("ListModels returned error: %v", err)
			}
			if len(models) != 1 || models[0] != "openai/gpt-4.1-mini" {
				t.Fatalf("model ids=%v", models)
			}
		})
	}
}

func TestListModels_OpenAICompatibleUsesAzureProjectDeploymentsWhenConfigured(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != http.MethodGet {
			t.Fatalf("request method = %s want GET", r.Method)
		}
		if r.URL.Path != "/api/projects/contact-5464/deployments" {
			t.Fatalf("request path = %s want /api/projects/contact-5464/deployments", r.URL.Path)
		}
		if got := r.URL.Query().Get("api-version"); got != "v1" {
			t.Fatalf("api-version = %q want v1", got)
		}
		if got := r.Header.Get("api-key"); got != "token_test" {
			t.Fatalf("api-key header = %q want token_test", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":[{"name":"Kimi-K2.6"},{"name":"gpt-4.1-mini"}]}`))
	}))
	defer srv.Close()

	t.Setenv("SWOBU_AZURE_OPENAI_PROJECT_ENDPOINT", srv.URL+"/api/projects/contact-5464")

	exec := NewExecutor(srv.Client(), stubCredentialResolver{}, NewOpenAICompatiblePolicy())
	target := exchange.NewRoutableTarget(
		"draft",
		"openai_compatible",
		"https://contact-5464-resource.openai.azure.com/openai/v1",
		"env:AZURE_OPENAI_API_KEY",
		protocolkind.ChatCompletions,
		"credential_ref",
		"",
		"",
	)
	target.AuthHeader = "api-key"
	models, err := exec.ListModels(context.Background(), target)
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if hits != 1 {
		t.Fatalf("project deployment hits = %d want 1", hits)
	}
	if len(models) != 2 || models[0] != "Kimi-K2.6" || models[1] != "gpt-4.1-mini" {
		t.Fatalf("model ids=%v", models)
	}
}
