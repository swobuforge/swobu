package openaicompat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
