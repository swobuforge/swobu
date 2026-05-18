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
	actions := (PollProviderAuthSessionEffect{
		EndpointName:   "acme",
		ProviderConfig: stateModel.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt"},
		AuthScope:      stateModel.AuthScopeEndpointProvider,
		SessionID:      "sess-1",
		AttemptsLeft:   5,
	}).Execute(context.Background())

	if len(actions) != 2 {
		t.Fatalf("actions length=%d want 2", len(actions))
	}
	if _, ok := actions[0].(ProviderAuthSessionPolledAction); !ok {
		t.Fatalf("action[0]=%T want ProviderAuthSessionPolledAction", actions[0])
	}
	failed, ok := actions[1].(ProviderAuthSessionFailedAction)
	if !ok {
		t.Fatalf("action[1]=%T want ProviderAuthSessionFailedAction", actions[1])
	}
	if got := strings.TrimSpace(failed.Message); got != "credential store failed" {
		t.Fatalf("failed message=%q", got)
	}
}
