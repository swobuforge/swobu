package openaifamily

import (
	"net/http"
	"testing"
)

func TestBearerAuthStrategy(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	BearerAuthStrategy().Apply(req, "tok_123")
	if got := req.Header.Get("Authorization"); got != "Bearer tok_123" {
		t.Fatalf("authorization=%q", got)
	}
}

func TestXAPIKeyAuthStrategy(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	XAPIKeyAuthStrategy().Apply(req, "tok_123")
	if got := req.Header.Get("X-API-Key"); got != "tok_123" {
		t.Fatalf("x-api-key=%q", got)
	}
}

func TestAPIKeyAuthStrategy(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	APIKeyAuthStrategy().Apply(req, "tok_123")
	if got := req.Header.Get("api-key"); got != "tok_123" {
		t.Fatalf("api-key=%q", got)
	}
}

func TestNoAuthStrategy(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	NoAuthStrategy().Apply(req, "tok_123")
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("authorization=%q", got)
	}
	if got := req.Header.Get("X-API-Key"); got != "" {
		t.Fatalf("x-api-key=%q", got)
	}
}

func TestAuthStrategyDoesNotOverwriteExplicitHeader(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	req.Header.Set("Authorization", "Bearer preexisting")
	BearerAuthStrategy().Apply(req, "tok_123")
	if got := req.Header.Get("Authorization"); got != "Bearer preexisting" {
		t.Fatalf("authorization=%q", got)
	}
}

func TestAuthStrategyForHeader_DefaultsToFallback(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	AuthStrategyForHeader("", BearerAuthStrategy()).Apply(req, "tok_123")
	if got := req.Header.Get("Authorization"); got != "Bearer tok_123" {
		t.Fatalf("authorization=%q", got)
	}
}

func TestAuthStrategyForHeader_UsesAuthorizationBearer(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	AuthStrategyForHeader("authorization", APIKeyAuthStrategy()).Apply(req, "tok_123")
	if got := req.Header.Get("Authorization"); got != "Bearer tok_123" {
		t.Fatalf("authorization=%q", got)
	}
}

func TestAuthStrategyForHeader_UsesApiKeyValueStyle(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	AuthStrategyForHeader("api-key", BearerAuthStrategy()).Apply(req, "tok_123")
	if got := req.Header.Get("api-key"); got != "tok_123" {
		t.Fatalf("api-key=%q", got)
	}
	req, _ = http.NewRequest(http.MethodGet, "https://example.test", nil)
	AuthStrategyForHeader("X-API-Key", BearerAuthStrategy()).Apply(req, "tok_123")
	if got := req.Header.Get("X-API-Key"); got != "tok_123" {
		t.Fatalf("x-api-key=%q", got)
	}
}

func TestAuthStrategyForHeader_UsesCustomHeaderValueStyle(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	AuthStrategyForHeader("X-Custom-Auth", BearerAuthStrategy()).Apply(req, "tok_123")
	if got := req.Header.Get("X-Custom-Auth"); got != "tok_123" {
		t.Fatalf("custom auth header=%q", got)
	}
}
