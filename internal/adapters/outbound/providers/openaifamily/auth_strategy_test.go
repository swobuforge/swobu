package openaifamily

import (
	"net/http"
	"testing"
)

func TestBearerAuthStrategy(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	bearerAuthStrategy().apply(req, "tok_123")
	if got := req.Header.Get("Authorization"); got != "Bearer tok_123" {
		t.Fatalf("authorization=%q", got)
	}
}

func TestXAPIKeyAuthStrategy(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	xAPIKeyAuthStrategy().apply(req, "tok_123")
	if got := req.Header.Get("X-API-Key"); got != "tok_123" {
		t.Fatalf("x-api-key=%q", got)
	}
}

func TestAPIKeyAuthStrategy(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	apiKeyAuthStrategy().apply(req, "tok_123")
	if got := req.Header.Get("api-key"); got != "tok_123" {
		t.Fatalf("api-key=%q", got)
	}
}

func TestNoAuthStrategy(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	noAuthStrategy().apply(req, "tok_123")
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
	bearerAuthStrategy().apply(req, "tok_123")
	if got := req.Header.Get("Authorization"); got != "Bearer preexisting" {
		t.Fatalf("authorization=%q", got)
	}
}

func TestAuthStrategyForHeader_DefaultsToFallback(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	authStrategyForHeader("", bearerAuthStrategy()).apply(req, "tok_123")
	if got := req.Header.Get("Authorization"); got != "Bearer tok_123" {
		t.Fatalf("authorization=%q", got)
	}
}

func TestAuthStrategyForHeader_UsesAuthorizationBearer(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	authStrategyForHeader("authorization", apiKeyAuthStrategy()).apply(req, "tok_123")
	if got := req.Header.Get("Authorization"); got != "Bearer tok_123" {
		t.Fatalf("authorization=%q", got)
	}
}

func TestAuthStrategyForHeader_UsesApiKeyValueStyle(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	authStrategyForHeader("api-key", bearerAuthStrategy()).apply(req, "tok_123")
	if got := req.Header.Get("api-key"); got != "tok_123" {
		t.Fatalf("api-key=%q", got)
	}
	req, _ = http.NewRequest(http.MethodGet, "https://example.test", nil)
	authStrategyForHeader("X-API-Key", bearerAuthStrategy()).apply(req, "tok_123")
	if got := req.Header.Get("X-API-Key"); got != "tok_123" {
		t.Fatalf("x-api-key=%q", got)
	}
}

func TestAuthStrategyForHeader_UsesCustomHeaderValueStyle(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	authStrategyForHeader("X-Custom-Auth", bearerAuthStrategy()).apply(req, "tok_123")
	if got := req.Header.Get("X-Custom-Auth"); got != "tok_123" {
		t.Fatalf("custom auth header=%q", got)
	}
}
