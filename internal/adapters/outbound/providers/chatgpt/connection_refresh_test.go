package chatgpt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	outboundcredentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestConnectionRefreshProducesBundleWithoutChangingCapturedSlot(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir())
	priorID := "a." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"fixture-account"}}`)) + ".c"
	original, err := outboundcredentials.EncodeTokenBundle(outboundcredentials.TokenBundle{AccessToken: "old-access", RefreshToken: "old-refresh", IDToken: priorID})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := outboundcredentials.StoreConnectionCredential("cn_refresh", original, outboundcredentials.CredentialWritePolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" {
			t.Error("wrong token exchange")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","expires_in":3600}`))
	}))
	defer server.Close()
	previous := chatGPTRefreshTokenURL
	chatGPTRefreshTokenURL = server.URL
	t.Cleanup(func() { chatGPTRefreshTokenURL = previous })
	id, _ := connectionidentity.Parse("cn_refresh")
	target, err := provider.NewTargetSnapshot("target", "chatgpt", server.URL, ref, protocolkind.Responses, "responses_stream", delivery.StreamingDelivery(delivery.FramingSSE)).BindConnection(id, 7)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewExecutor(server.Client(), outboundcredentials.NewResolver())
	refreshed, err := adapter.RefreshConnectionBundle(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	bundle, ok, err := outboundcredentials.DecodeTokenBundle(refreshed)
	if err != nil || !ok || bundle.AccessToken != "new-access" || bundle.RefreshToken != "old-refresh" || bundle.IDToken != priorID {
		t.Fatal("refresh lost routing or refresh material", err)
	}
	current, err := outboundcredentials.ResolveConnectionStoredBundle("cn_refresh", ref)
	if err != nil || current != original || calls != 1 {
		t.Fatal("refresh changed captured immutable slot", err)
	}
	other, _ := connectionidentity.Parse("cn_other")
	foreign, err := provider.NewTargetSnapshot("target", "chatgpt", server.URL, ref, protocolkind.Responses, "responses_stream", delivery.StreamingDelivery(delivery.FramingSSE)).BindConnection(other, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.RefreshConnectionBundle(t.Context(), foreign); err == nil || calls != 1 {
		t.Fatal("foreign owner reached token endpoint")
	}
	if _, err := NewExecutor(server.Client(), stubCredentialResolver{}).RefreshConnectionBundle(t.Context(), target); err == nil || calls != 1 {
		t.Fatal("provider-only resolver acquired refresh authority")
	}
}

// A reusable Connection denotes one account. A token response cannot silently
// turn a credential refresh into an account or regulated-endpoint switch.
func TestConnectionRefreshRejectsRoutingIdentityChange(t *testing.T) {
	for _, tc := range []struct {
		name, account string
		fed           bool
	}{
		{"different account", "other-account", false},
		{"regulated endpoint", "fixture-account", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SWOBU_HOME", t.TempDir())
			token := func(account string, fed bool) string {
				raw, _ := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": account, "chatgpt_account_is_fedramp": fed}})
				return "a." + base64.RawURLEncoding.EncodeToString(raw) + ".c"
			}
			original, err := outboundcredentials.EncodeTokenBundle(outboundcredentials.TokenBundle{AccessToken: "old-access", RefreshToken: "old-refresh", IDToken: token("fixture-account", false)})
			if err != nil {
				t.Fatal(err)
			}
			ref, err := outboundcredentials.StoreConnectionCredential("cn_refresh", original, outboundcredentials.CredentialWritePolicyFile)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "id_token": token(tc.account, tc.fed)})
			}))
			defer server.Close()
			previous := chatGPTRefreshTokenURL
			chatGPTRefreshTokenURL = server.URL
			t.Cleanup(func() { chatGPTRefreshTokenURL = previous })
			id, _ := connectionidentity.Parse("cn_refresh")
			target, err := provider.NewTargetSnapshot("target", "chatgpt", server.URL, ref, protocolkind.Responses, "responses_stream", delivery.StreamingDelivery(delivery.FramingSSE)).BindConnection(id, 7)
			if err != nil {
				t.Fatal(err)
			}
			adapter := NewExecutor(server.Client(), outboundcredentials.NewResolver())
			result, err := adapter.RefreshConnectionBundle(t.Context(), target)
			if err == nil || result != "" {
				t.Fatal("refresh accepted a changed routing identity")
			}
			current, err := outboundcredentials.ResolveConnectionStoredBundle("cn_refresh", ref)
			if err != nil || current != original {
				t.Fatal("rejected refresh changed captured slot", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if result, err := adapter.RefreshConnectionBundle(ctx, target); result != "" || !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}
