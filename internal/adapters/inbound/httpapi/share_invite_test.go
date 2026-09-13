package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShareInviteIsSelfContainedAndSecurityBounded(t *testing.T) {
	recorder := httptest.NewRecorder()
	serveShareInvite(recorder)
	response := recorder.Result()
	body := recorder.Body.String()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	for header, want := range map[string]string{
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; frame-ancestors 'none'",
	} {
		if got := response.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	for _, required := range []string{
		"Shared API",
		`<link rel="icon" href="data:image/svg+xml,`,
		"Incomplete share link",
		"Copy complete OpenAI connection recipe",
		"Copy complete Anthropic connection recipe",
		"OPENAI_BASE_URL=${urls.openai}",
		"ANTHROPIC_BASE_URL=${urls.anthropic}",
		"history.replaceState(null, '', location.pathname + location.search)",
		"document.addEventListener('DOMContentLoaded', bindInvite, { once: true })",
		"code.addData(originalInvite, 'Byte')",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("invite missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`<link rel="stylesheet"`,
		`<script src=`,
		"https://cdnjs.cloudflare.com",
		"SWOBU_STYLES */",
		"SWOBU_QRCODE */",
		"SWOBU_APP */",
		"SwobuQR",
		"/_swobu/share-info",
		"fetch(",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("invite retained external or custom implementation marker %q", forbidden)
		}
	}

	capture := strings.Index(body, "const rawHash = location.hash")
	completeInvite := strings.Index(body, "const originalInvite = location.href")
	scrub := strings.Index(body, "history.replaceState")
	if capture < 0 || completeInvite < capture || scrub < completeInvite {
		t.Fatalf("unsafe fragment sequence: capture=%d invite=%d scrub=%d", capture, completeInvite, scrub)
	}
}
