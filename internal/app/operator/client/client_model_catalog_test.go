package operatorclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientProbeModelCatalogEncodesQueryParams(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/_swobu/model-catalog" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("provider_spec"); got != "openai" {
			t.Fatalf("provider_spec = %q, want openai", got)
		}
		if got := r.URL.Query().Get("base_url"); got != "https://api.openai.com/v1" {
			t.Fatalf("base_url = %q, want https://api.openai.com/v1", got)
		}
		if got := r.URL.Query().Get("auth_header"); got != "Authorization" {
			t.Fatalf("auth_header = %q, want Authorization", got)
		}
		if got := r.URL.Query().Get("credential_ref"); got != "env:OPENAI_API_KEY" {
			t.Fatalf("credential_ref = %q, want env:OPENAI_API_KEY", got)
		}
		if got := r.URL.Query().Get("auth_mode"); got != "env" {
			t.Fatalf("auth_mode = %q, want env", got)
		}
		if got := r.URL.Query().Get("provider_protocol"); got != "responses" {
			t.Fatalf("provider_protocol = %q, want responses", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"deployments":[
				{"name":"gpt-4.1","model_name":"gpt-4.1","model_publisher":"openai","model_version":"2024-01","family":"gpt","supported_provider_protocols":["responses","chat_completions"],"default_provider_protocol":"responses"},
				{"name":"gpt-4o","model_name":"gpt-4o","model_publisher":"openai","family":"gpt","default_provider_protocol":"responses"}
			],
			"resolved_provider_protocol":"responses"
		}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	result, err := c.ProbeModelCatalog(context.Background(), "openai", "https://api.openai.com/v1", "Authorization", "env:OPENAI_API_KEY", "env", "responses")
	if err != nil {
		t.Fatalf("ProbeModelCatalog returned error: %v", err)
	}
	if len(result.Deployments) != 2 {
		t.Fatalf("deployments = %d, want 2", len(result.Deployments))
	}
	d0 := result.Deployments[0]
	if d0.Name != "gpt-4.1" || d0.ModelName != "gpt-4.1" || d0.ModelPublisher != "openai" || d0.ModelVersion != "2024-01" || d0.Family != "gpt" || d0.DefaultProviderProtocol != "responses" {
		t.Fatalf("first deployment = %#v", d0)
	}
	if len(d0.SupportedProviderProtocols) != 2 || d0.SupportedProviderProtocols[0] != "responses" || d0.SupportedProviderProtocols[1] != "chat_completions" {
		t.Fatalf("first deployment protocols = %v", d0.SupportedProviderProtocols)
	}
	d1 := result.Deployments[1]
	if d1.Name != "gpt-4o" || d1.ModelName != "gpt-4o" {
		t.Fatalf("second deployment = %#v", d1)
	}
	if result.ResolvedProviderProtocol != "responses" {
		t.Fatalf("resolved_provider_protocol = %q, want responses", result.ResolvedProviderProtocol)
	}
}

func TestClientProbeModelCatalog404ReturnsError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"unknown provider"}}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	_, err := c.ProbeModelCatalog(context.Background(), "unknown", "", "", "", "", "")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("error = %q, want 'unknown provider'", err.Error())
	}
}

func TestClientProbeModelCatalog500ReturnsError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL","message":"backend unavailable"}}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	_, err := c.ProbeModelCatalog(context.Background(), "openai", "", "", "", "", "")
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "backend unavailable") {
		t.Fatalf("error = %q, want 'backend unavailable'", err.Error())
	}
}
