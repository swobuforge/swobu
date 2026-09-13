package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/sharestate"
)

func TestShareHandlerRecognizesCORSPreflightBeforeDependencies(t *testing.T) {
	for _, test := range []struct {
		name             string
		path             string
		origin           string
		requestedHeaders string
	}{
		{
			name:             "captured TypingMind Responses request",
			path:             "/v1/responses",
			origin:           "https://www.typingmind.com",
			requestedHeaders: "authorization,content-type",
		},
		{
			name:             "Responses x-api-key contract",
			path:             "/v1/responses",
			origin:           "https://client.example",
			requestedHeaders: "x-api-key,content-type",
		},
		{
			name:             "Messages x-api-key contract",
			path:             "/v1/messages",
			origin:           "https://client.example",
			requestedHeaders: "x-api-key,anthropic-version,content-type",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodOptions, test.path, nil)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Access-Control-Request-Method", http.MethodPost)
			request.Header.Set("Access-Control-Request-Headers", test.requestedHeaders)
			response := httptest.NewRecorder()

			(ShareHandler{}).ServeHTTP(response, request)

			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
			}
			if response.Body.Len() != 0 {
				t.Fatalf("body = %q, want empty", response.Body.String())
			}
			assertShareCORSHeaders(t, response.Header())
			if challenge := response.Header().Get("WWW-Authenticate"); challenge != "" {
				t.Fatalf("WWW-Authenticate = %q, want absent", challenge)
			}
		})
	}
}

func TestShareHandlerCORSIsStatic(t *testing.T) {
	request := httptest.NewRequest(http.MethodOptions, "/v1/responses", nil)
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodDelete)
	request.Header.Set("Access-Control-Request-Headers", "authorization,x-arbitrary")
	response := httptest.NewRecorder()

	(ShareHandler{}).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	assertShareCORSHeaders(t, response.Header())
	if got := strings.ToLower(response.Header().Get("Access-Control-Allow-Headers")); strings.Contains(got, "x-arbitrary") {
		t.Fatalf("Access-Control-Allow-Headers reflected request: %q", got)
	}
	for _, header := range []string{"Access-Control-Allow-Credentials", "Access-Control-Max-Age"} {
		if got := response.Header().Get(header); got != "" {
			t.Fatalf("%s = %q, want absent", header, got)
		}
	}
}

func TestShareHandlerDoesNotBypassAuthenticationForNonPreflight(t *testing.T) {
	shareStore, err := sharestate.Open(filepath.Join(t.TempDir(), "share.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewShareHandler(shareStore, nil, exchange.RequestIngress{}, nil)

	for _, test := range []struct {
		name   string
		method string
		origin string
		access string
	}{
		{name: "naked options", method: http.MethodOptions},
		{name: "origin only", method: http.MethodOptions, origin: "https://www.typingmind.com"},
		{name: "requested method only", method: http.MethodOptions, access: http.MethodPost},
		{name: "actual request", method: http.MethodPost, origin: "https://www.typingmind.com"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/v1/responses", nil)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Access-Control-Request-Method", test.access)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			if got := response.Header().Get("WWW-Authenticate"); got != `Bearer realm="swobu-share"` {
				t.Fatalf("WWW-Authenticate = %q", got)
			}
			assertShareCORSHeaders(t, response.Header())
		})
	}
}

func TestShareHandlerAuthenticatedModelsRetainProjectionAndCORS(t *testing.T) {
	workspace := shareModelsWorkspace(t)
	configStore := openShareHandlerConfigStore(t, workspace)
	shareStore, err := sharestate.Open(filepath.Join(t.TempDir(), "share.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := shareStore.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	grant, err := shareStore.Issue(workspace.Slug(), routing.RouteName{}, sharestate.ExpiryOneDay)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer "+grant.Bearer)
	response := httptest.NewRecorder()

	NewShareHandler(shareStore, configStore, exchange.RequestIngress{}, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, model := range []string{`"id":"coding"`, `"id":"writing"`} {
		if !strings.Contains(response.Body.String(), model) {
			t.Errorf("models response missing %s: %s", model, response.Body.String())
		}
	}
	assertShareCORSHeaders(t, response.Header())
}

func TestShareHandlerAuthenticatedResponsesRequestRetainsCORS(t *testing.T) {
	workspace := shareModelsWorkspace(t)
	configStore := openShareHandlerConfigStore(t, workspace)
	shareStore, err := sharestate.Open(filepath.Join(t.TempDir(), "share.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := shareStore.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	coding, _ := routing.ParseRouteName("coding")
	grant, err := shareStore.Issue(workspace.Slug(), coding, sharestate.ExpiryOneDay)
	if err != nil {
		t.Fatal(err)
	}
	runtime := terminalProjectionRuntime{
		RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(),
		transport: terminalProjectionTransport{
			documentBody: `{"id":"resp","model":"coding-model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"1"}]}]}`,
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"coding","input":"Reply 1"}`))
	request.Header.Set("Origin", "https://www.typingmind.com")
	request.Header.Set("Authorization", "Bearer "+grant.Bearer)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	NewShareHandler(shareStore, configStore, exchange.NewIngress(nil, runtime, exchange.RuntimePoliciesSpec{}), nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"text":"1"`) {
		t.Fatalf("response did not traverse Responses ingress: %s", response.Body.String())
	}
	assertShareCORSHeaders(t, response.Header())
}

func TestShareHandlerInviteRetainsSecurityHeadersAndAddsCORS(t *testing.T) {
	response := httptest.NewRecorder()
	(ShareHandler{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	for header, want := range map[string]string{
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; frame-ancestors 'none'",
	} {
		if got := response.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	assertShareCORSHeaders(t, response.Header())
}

func TestLocalHandlerDoesNotEmitShareCORS(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler(nil, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/unsupported", nil))

	for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
		if got := response.Header().Get(header); got != "" {
			t.Fatalf("local %s = %q, want absent", header, got)
		}
	}
}

func openShareHandlerConfigStore(t *testing.T, workspace routing.Workspace) *configstore.Store {
	t.Helper()
	configDir := t.TempDir()
	if err := os.Chmod(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.OpenOrCreate(filepath.Join(configDir, "swobu.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.Update(context.Background(), func(routing.Config) (routing.Config, error) {
		return routing.NewConfig([]routing.Workspace{workspace})
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

func assertShareCORSHeaders(t *testing.T, header http.Header) {
	t.Helper()
	if got := header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	assertHeaderSet(t, header, "Access-Control-Allow-Methods", []string{"GET", "POST"})
	assertHeaderSet(t, header, "Access-Control-Allow-Headers", []string{"authorization", "x-api-key", "content-type", "anthropic-version"})
}

func assertHeaderSet(t *testing.T, header http.Header, name string, want []string) {
	t.Helper()
	got := strings.Split(strings.ToLower(header.Get(name)), ",")
	for i := range got {
		got[i] = strings.TrimSpace(got[i])
	}
	normalizedWant := make([]string, len(want))
	for i := range want {
		normalizedWant[i] = strings.ToLower(strings.TrimSpace(want[i]))
	}
	slices.Sort(got)
	slices.Sort(normalizedWant)
	if !slices.Equal(got, normalizedWant) {
		t.Errorf("%s = %v, want %v", name, got, normalizedWant)
	}
}
