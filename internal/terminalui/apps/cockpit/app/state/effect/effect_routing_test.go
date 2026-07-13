package effect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func TestPollProviderAuthSessionEffect_FailedStatusSurfacesCredentialStoreError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/_swobu/auth/sessions/sess-1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"provider_spec":"chatgpt","session_id":"sess-1","state":"failed","credential_ref":"","error":"credential store failed"}`))
	}))
	defer srv.Close()

	t.Setenv("SWOBU_DAEMON_URL", srv.URL)
	action := (PollProviderAuthSessionEffect{
		EndpointName:   "acme",
		ProviderConfig: stateModel.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt"},
		AuthScope:      stateModel.AuthScopeEndpointProvider,
		SessionID:      "sess-1",
		AttemptsLeft:   5,
	}).Run(context.Background())

	failed, ok := action.(ProviderAuthSessionFailedAction)
	if !ok {
		t.Fatalf("action=%T want ProviderAuthSessionFailedAction", action)
	}
	if got := strings.TrimSpace(failed.Message); got != "credential store failed" { // swobu:io-string source=domain
		t.Fatalf("failed message=%q", got)
	}
}
