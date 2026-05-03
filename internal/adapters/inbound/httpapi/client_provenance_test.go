package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func TestClassifyClientHandler_KnownSignatures(t *testing.T) {
	tests := []struct {
		name      string
		client_ua string
		headers   map[string]string
		want      string
	}{
		{name: "codex", client_ua: "Codex/1.0", want: "codex"},
		{name: "claude code", client_ua: "Claude-Code/2.0", want: "claude_code"},
		{name: "hermes", client_ua: "Hermes CLI", want: "hermes"},
		{name: "stainless lang fallback", client_ua: "", headers: map[string]string{"X-Stainless-Lang": "python"}, want: "stainless_python"},
		{name: "anthropic sdk product token", client_ua: "anthropic-sdk-python/0.58", want: "anthropic_sdk_python"},
		{name: "curl", client_ua: "curl/8.1.2", want: "curl"},
		{name: "unknown product token", client_ua: "my-local-client", want: "my_local_client"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", nil)
			req.Header.Set("User-Agent", tc.client_ua)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if got := classifyClientHandler(req); got != tc.want {
				t.Fatalf("classifyClientHandler() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIngressProvenance_MapsProtocolAndOperation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/messages", nil)
	req.Header.Set("User-Agent", "Claude-Code/2.0")
	provenance := ingressProvenance(req, compatibility.IngressFamilyMessages, compatibility.NormalizedPathMessages)
	if provenance.ClientProtocol != "anthropic_compat" {
		t.Fatalf("client protocol = %q, want %q", provenance.ClientProtocol, "anthropic_compat")
	}
	if provenance.ClientHandler != "claude_code" {
		t.Fatalf("client handler = %q, want %q", provenance.ClientHandler, "claude_code")
	}
}
