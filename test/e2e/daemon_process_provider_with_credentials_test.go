// Package e2e_test owns end-to-end driver tests for Swobu daemon process.
package e2e_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	keyringcommodity "github.com/zalando/go-keyring"

	credentialsadapter "github.com/metrofun/swobu/internal/adapters/outbound/credentials"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestDaemonProcessProviderWithCredentials_EnvCredentialResolves(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key-12345" {
			t.Fatalf("Authorization header = %q, want Bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_cred","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"auth-ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	testAPIKey := "test-api-key-12345"
	t.Setenv("OPENAI_API_KEY", testAPIKey)

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(
				t,
				"cred-test",
				"cred-backend",
				mustProviderConfigWithModelID(
					t,
					harness.NewProviderConfig(
						t,
						"cred-backend",
						"openai",
						upstream.URL+"/v1",
						"env",
						protocolsurface.ChatCompletions,
					),
					"gpt-4.1-mini",
				),
			),
		},
	})

	req, err := http.NewRequest(http.MethodPost, daemon.BaseURL+"/c/cred-test/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(raw))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"auth-ok"`)) {
		t.Fatalf("body = %s, want upstream success payload", string(raw))
	}
}

func TestDaemonProcessProviderWithCredentials_KeychainCredentialResolves(t *testing.T) {
	const providerSpec = "openai"
	const keyName = "openai/e2e"
	const token = "keychain-token-123"
	scope := credentialsadapter.KeyringScopeForProvider(providerSpec)
	if err := keyringcommodity.Set(scope, keyName, token); err != nil {
		t.Skipf("keyring unavailable in this environment: %v", err)
	}
	t.Cleanup(func() {
		_ = keyringcommodity.Delete(scope, keyName)
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization header = %q, want Bearer %s", got, token)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_cred","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"auth-ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(
				t,
				"cred-test",
				"cred-backend",
				mustProviderConfigWithModelID(
					t,
					harness.NewProviderConfig(
						t,
						"cred-backend",
						providerSpec,
						upstream.URL+"/v1",
						"keychain:"+keyName,
						protocolsurface.ChatCompletions,
					),
					"gpt-4.1-mini",
				),
			),
		},
	})

	req, err := http.NewRequest(http.MethodPost, daemon.BaseURL+"/c/cred-test/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(raw))
	}
}
