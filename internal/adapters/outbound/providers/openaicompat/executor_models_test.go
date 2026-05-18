package openaicompat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ports"
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

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	_, err := exec.ListModels(context.Background(), ports.NewRoutableTarget(
		"draft",
		"openrouter",
		srv.URL+"/v1",
		"env:OPENROUTER_API_KEY",
		protocolkind.ChatCompletions,
		"credential_ref",
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

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	_, err := exec.ListModels(context.Background(), ports.NewRoutableTarget(
		"draft",
		"openai",
		srv.URL+"/v1",
		"",
		protocolkind.ChatCompletions,
		"credential_ref",
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

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	_, err := exec.ListModels(context.Background(), ports.NewRoutableTarget(
		"draft",
		"openrouter",
		srv.URL+"/v1",
		"",
		protocolkind.ChatCompletions,
		"credential_ref",
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
