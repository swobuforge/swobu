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
