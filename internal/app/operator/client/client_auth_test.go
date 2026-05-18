package operatorclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientStartAuthSession(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/_swobu/auth/sessions" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"provider_spec":"chatgpt","session_id":"sess-1","authorize_url":"https://auth.example","expires_at":"2026-05-18T12:00:00Z","state":"pending"}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	out, err := c.StartAuthSession(context.Background(), "chatgpt", "main/openai", "browser")
	if err != nil {
		t.Fatalf("StartAuthSession returned error: %v", err)
	}
	if out.ProviderSpec != "chatgpt" || out.SessionID != "sess-1" || out.State != "pending" || out.ExpiresAt != "2026-05-18T12:00:00Z" {
		t.Fatalf("unexpected output: %#v", out)
	}
}

func TestClientGetAuthSessionStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/_swobu/auth/sessions/sess-1" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"provider_spec":"chatgpt","session_id":"sess-1","state":"succeeded","credential_ref":"keychain:chatgpt/default","error":""}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	out, err := c.GetAuthSessionStatus(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetAuthSessionStatus returned error: %v", err)
	}
	if out.ProviderSpec != "chatgpt" || out.CredentialRef != "keychain:chatgpt/default" || out.State != "succeeded" {
		t.Fatalf("unexpected output: %#v", out)
	}
}

func TestClientCancelAuthSession(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/_swobu/auth/sessions/sess-1/cancel" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	if err := c.CancelAuthSession(context.Background(), "sess-1"); err != nil {
		t.Fatalf("CancelAuthSession returned error: %v", err)
	}
}

func TestClientRetryAuthSession(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/_swobu/auth/sessions/sess-1/retry" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_id":"sess-2","authorize_url":"https://auth.example","expires_at":"2026-05-18T12:05:00Z","state":"pending"}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	out, err := c.RetryAuthSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("RetryAuthSession returned error: %v", err)
	}
	if out.SessionID != "sess-2" || out.State != "pending" || out.ExpiresAt != "2026-05-18T12:05:00Z" {
		t.Fatalf("unexpected output: %#v", out)
	}
}
