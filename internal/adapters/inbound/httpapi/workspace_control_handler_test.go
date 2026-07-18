package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
)

func TestWorkspaceControlHandlerCreatesAndMutatesSemanticResources(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o700)
	path := filepath.Join(dir, "swobu.yaml")
	_ = os.WriteFile(path, []byte("schema_version: 1\nworkspaces: {}\n"), 0o600)
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := workspaces.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWorkspaceControlHandler(service)
	body := `{"slug":"dev","initial_route":"chat","target":{"id":"a","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:OPENAI_API_KEY"}}}}`
	request := httptest.NewRequest(http.MethodPost, "/_swobu/workspaces", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"default_route":"chat"`) {
		t.Fatalf("create response=%s", response.Body.String())
	}
	credential := httptest.NewRequest(http.MethodPost, "/_swobu/workspaces/dev/routes/chat/targets/a/credential", strings.NewReader(`{"credential":"env:ROTATED"}`))
	credentialResponse := httptest.NewRecorder()
	handler.ServeHTTP(credentialResponse, credential)
	if credentialResponse.Code != http.StatusOK || !strings.Contains(credentialResponse.Body.String(), "env:ROTATED") {
		t.Fatalf("credential status=%d body=%s", credentialResponse.Code, credentialResponse.Body.String())
	}
}

func TestWorkspaceControlRouterUsesStandardGETHeadSemantics(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o700)
	path := filepath.Join(dir, "swobu.yaml")
	_ = os.WriteFile(path, []byte("schema_version: 1\nworkspaces: {}\n"), 0o600)
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := workspaces.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWorkspaceControlHandler(service)
	request := httptest.NewRequest(http.MethodHead, "/_swobu/workspaces", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want standard GET-pattern match", response.Code)
	}
}

func TestWorkspaceControlRouterRegistersEverySemanticCommand(t *testing.T) {
	mux := NewWorkspaceControlHandler(workspaces.Service{})
	tests := []struct {
		method, path, pattern string
	}{
		{http.MethodGet, "/_swobu/workspaces", "GET /_swobu/workspaces"},
		{http.MethodPost, "/_swobu/workspaces", "POST /_swobu/workspaces"},
		{http.MethodGet, "/_swobu/workspaces/dev", "GET /_swobu/workspaces/{workspace}"},
		{http.MethodDelete, "/_swobu/workspaces/dev", "DELETE /_swobu/workspaces/{workspace}"},
		{http.MethodPost, "/_swobu/workspaces/dev/rename", "POST /_swobu/workspaces/{workspace}/rename"},
		{http.MethodPost, "/_swobu/workspaces/dev/default-route", "POST /_swobu/workspaces/{workspace}/default-route"},
		{http.MethodPost, "/_swobu/workspaces/dev/routes", "POST /_swobu/workspaces/{workspace}/routes"},
		{http.MethodDelete, "/_swobu/workspaces/dev/routes/chat", "DELETE /_swobu/workspaces/{workspace}/routes/{route}"},
		{http.MethodPost, "/_swobu/workspaces/dev/routes/chat/rename", "POST /_swobu/workspaces/{workspace}/routes/{route}/rename"},
		{http.MethodPost, "/_swobu/workspaces/dev/routes/chat/targets", "POST /_swobu/workspaces/{workspace}/routes/{route}/targets"},
		{http.MethodPut, "/_swobu/workspaces/dev/routes/chat/targets/a", "PUT /_swobu/workspaces/{workspace}/routes/{route}/targets/{target}"},
		{http.MethodDelete, "/_swobu/workspaces/dev/routes/chat/targets/a", "DELETE /_swobu/workspaces/{workspace}/routes/{route}/targets/{target}"},
		{http.MethodPost, "/_swobu/workspaces/dev/routes/chat/targets/a/credential", "POST /_swobu/workspaces/{workspace}/routes/{route}/targets/{target}/credential"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, nil)
		_, pattern := mux.Handler(request)
		if pattern != test.pattern {
			t.Errorf("%s %s pattern = %q, want %q", test.method, test.path, pattern, test.pattern)
		}
	}
}

func TestWorkspaceControlHandlerRejectsWholeWorkspaceReplacement(t *testing.T) {
	handler := NewWorkspaceControlHandler(workspaces.Service{})
	request := httptest.NewRequest(http.MethodPut, "/_swobu/workspaces/dev", strings.NewReader(`{"routes":[]}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", response.Code)
	}
	if response.Header().Get("Allow") != "DELETE, GET, HEAD" {
		t.Fatalf("Allow = %q", response.Header().Get("Allow"))
	}
}

func TestWorkspaceControlHandlerRejectsMalformedTrailingJSON(t *testing.T) {
	handler := NewWorkspaceControlHandler(workspaces.Service{})
	body := `{"slug":"dev","initial_route":"chat","target":{"id":"a","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:KEY"}}}} garbage`
	request := httptest.NewRequest(http.MethodPost, "/_swobu/workspaces", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestWorkspaceControlHandlerUpdateRejectsBodyIdentityAndProvider(t *testing.T) {
	handler := NewWorkspaceControlHandler(workspaces.Service{})
	for name, body := range map[string]string{
		"body id":   `{"target":{"id":"other","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:KEY"}}}}`,
		"provider":  `{"target":{"model":"gpt-5","protocol":"responses","provider":"openai","connection":{"openai":{"credential":"env:KEY"}}}}`,
		"placement": `{"target":{"model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:KEY"}}},"placement":{"balance_with":"other"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/_swobu/workspaces/dev/routes/chat/targets/a", strings.NewReader(body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestWorkspaceCommandsRoundTripProviderArmsAcrossStoreReopen(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workspaces.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWorkspaceControlHandler(service)

	create := `{"slug":"dev","initial_route":"chat","target":{"id":"openai","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:OPENAI_API_KEY"}}}}`
	assertWorkspaceCommandStatus(t, handler, http.MethodPost, "/_swobu/workspaces", create, http.StatusCreated)

	routes := map[string]string{
		"anthropic":  `{"id":"anthropic","model":"claude","protocol":"messages","connection":{"anthropic":{"credential":"env:ANTHROPIC_API_KEY"}}}`,
		"openrouter": `{"id":"openrouter","model":"openai/gpt-5","protocol":"chat_completions","connection":{"openrouter":{"credential":"keychain:openrouter/default"}}}`,
		"chatgpt":    `{"id":"chatgpt","model":"gpt-5","protocol":"responses_stream","connection":{"chatgpt":{"credential":"secretfile:chatgpt/default"}}}`,
		"ollama":     `{"id":"ollama","model":"llama","protocol":"chat_completions","connection":{"ollama":{}}}`,
		"azure":      `{"id":"azure","model":"deployment","protocol":"responses","connection":{"azure":{"project_endpoint":"https://example.services.ai.azure.com/api/projects/prod","credential":"env:AZURE_KEY"}}}`,
		"bedrock":    `{"id":"bedrock","model":"openai.gpt","protocol":"responses_stream","connection":{"bedrock":{"region":"eu-west-2","auth":{"environment":{}}}}}`,
		"custom":     `{"id":"custom","model":"local","protocol":"chat_completions","connection":{"custom":{"base_url":"https://example.test/v1","header":{"name":"Authorization","credential":"env:CUSTOM_KEY"}}}}`,
	}
	for route, target := range routes {
		body := `{"name":"` + route + `","target":` + target + `}`
		assertWorkspaceCommandStatus(t, handler, http.MethodPost, "/_swobu/workspaces/dev/routes", body, http.StatusOK)
	}

	// Typed command failures must not remove or partially publish the final target.
	conflict := assertWorkspaceCommandStatus(t, handler, http.MethodDelete, "/_swobu/workspaces/dev/routes/chat/targets/openai", "", http.StatusConflict)
	if !strings.Contains(conflict, `"code":"CONFLICT"`) || !strings.Contains(conflict, "delete the route instead") {
		t.Fatalf("last-target conflict response = %s", conflict)
	}
	assertWorkspaceCommandStatus(t, handler, http.MethodPost, "/_swobu/workspaces/dev/routes/chat/targets/openai/credential", `{"credential":"env:"}`, http.StatusBadRequest)

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopenedService, err := workspaces.NewService(reopened)
	if err != nil {
		t.Fatal(err)
	}
	reopenedHandler := NewWorkspaceControlHandler(reopenedService)
	request := httptest.NewRequest(http.MethodGet, "/_swobu/workspaces/dev", nil)
	response := httptest.NewRecorder()
	reopenedHandler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("reopen GET status=%d body=%s", response.Code, response.Body.String())
	}
	for _, provider := range []string{"openai", "anthropic", "openrouter", "chatgpt", "ollama", "azure", "bedrock", "custom"} {
		if !strings.Contains(response.Body.String(), `"provider":"`+provider+`"`) {
			t.Errorf("reopened response missing provider %q: %s", provider, response.Body.String())
		}
	}
	if !strings.Contains(response.Body.String(), `"credential":"env:OPENAI_API_KEY"`) {
		t.Fatal("failed credential mutation changed committed OpenAI credential")
	}
}

func assertWorkspaceCommandStatus(t *testing.T, handler http.Handler, method, path, body string, want int) string {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.Code, want, response.Body.String())
	}
	return response.Body.String()
}
