package openaifamily

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
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
	_, err := exec.ListDeployments(context.Background(), provider.NewTargetSnapshot(
		"draft",
		"openrouter",
		srv.URL+"/v1",
		"env:OPENROUTER_API_KEY",
		protocolkind.ChatCompletions,

		"",
		""))
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
	_, err := exec.ListDeployments(context.Background(), provider.NewTargetSnapshot(
		"draft",
		"openai",
		srv.URL+"/v1",
		"",
		protocolkind.ChatCompletions,

		"",
		""))
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
	_, err := exec.ListDeployments(context.Background(), provider.NewTargetSnapshot(
		"draft",
		"openrouter",
		srv.URL+"/v1",
		"",
		protocolkind.ChatCompletions,

		"",
		""))
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

func TestListModels_LMStudioUsesNativeCatalogAndOptionalBearerAuth(t *testing.T) {
	for _, tc := range []struct {
		name          string
		credentialRef string
		wantAuth      string
	}{
		{name: "unauthenticated"},
		{name: "bearer token", credentialRef: "env:LM_API_TOKEN", wantAuth: "Bearer token_test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/models" {
					t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != tc.wantAuth {
					t.Fatalf("Authorization = %q, want %q", got, tc.wantAuth)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"models":[{"type":"llm","key":"local-model","display_name":"Local Model","publisher":"Acme","architecture":"qwen"}]}`))
			}))
			defer srv.Close()
			exec := NewExecutor(srv.Client(), stubCredentialResolver{}, NewLMStudioPolicy())
			models, err := exec.ListDeployments(context.Background(), provider.NewTargetSnapshot(
				"draft", "lmstudio", srv.URL+"/v1", tc.credentialRef,
				protocolkind.Responses, "", "responses"))
			if err != nil {
				t.Fatal(err)
			}
			if len(models) != 1 || models[0].Name != "local-model" || models[0].ModelName != "Local Model" || models[0].ModelPublisher != "Acme" || models[0].Family != "qwen" {
				t.Fatalf("models = %#v", models)
			}
		})
	}
}

func TestListModels_VLLMUsesSparseOpenAICatalogAndOptionalBearerAuth(t *testing.T) {
	for _, tc := range []struct {
		name          string
		credentialRef string
		wantAuth      string
	}{
		{name: "unauthenticated"},
		{name: "bearer token", credentialRef: "env:VLLM_API_KEY", wantAuth: "Bearer token_test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/serve/models" {
					t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != tc.wantAuth {
					t.Fatalf("Authorization = %q, want %q", got, tc.wantAuth)
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"served/model"}]}`))
			}))
			defer srv.Close()

			exec := NewExecutor(srv.Client(), stubCredentialResolver{}, NewVLLMPolicy())
			models, err := exec.ListDeployments(context.Background(), provider.NewTargetSnapshot(
				"draft", "vllm", srv.URL+"/serve", tc.credentialRef,
				protocolkind.Responses, "", "responses"))
			if err != nil {
				t.Fatal(err)
			}
			if len(models) != 1 || models[0].Name != "served/model" || len(models[0].SupportedProviderProtocols) != 0 || models[0].DefaultProviderProtocol != "" {
				t.Fatalf("models = %#v", models)
			}
			want := []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"}
			if got := profile.ResolveProviderDeployment("vllm", models[0]).ProtocolOptions(); !slices.Equal(got, want) {
				t.Fatalf("resolved protocols = %#v, want %#v", got, want)
			}
		})
	}
}

func TestListModels_CustomUsesSelectedAuthHeader(t *testing.T) {
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

			exec := NewExecutor(srv.Client(), stubCredentialResolver{}, NewCustomPolicy())
			// Auth header is a custom-endpoint fact fixed at construction via
			// NewCustomTargetSnapshot (never post-construction mutation).
			target := provider.NewCustomTargetSnapshot(
				"draft",
				srv.URL+"/v1",
				"env:OPENAI_API_KEY",
				protocolkind.ChatCompletions,

				"",
				"",
				tt.authHeader)

			models, err := exec.ListDeployments(context.Background(), target)
			if err != nil {
				t.Fatalf("ListDeployments returned error: %v", err)
			}
			if len(models) != 1 || models[0].Name != "openai/gpt-4.1-mini" {
				t.Fatalf("deployments=%v", models)
			}
		})
	}
}
