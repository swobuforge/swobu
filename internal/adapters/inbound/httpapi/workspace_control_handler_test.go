package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
)

func TestWorkspaceControlHandlerCreatesAndReadsSemanticResources(t *testing.T) {
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
	route := httptest.NewRequest(http.MethodGet, "/_swobu/workspaces/dev/routes/chat", nil)
	routeResponse := httptest.NewRecorder()
	handler.ServeHTTP(routeResponse, route)
	if routeResponse.Code != http.StatusOK || !strings.Contains(routeResponse.Body.String(), "env:OPENAI_API_KEY") {
		t.Fatalf("route status=%d body=%s", routeResponse.Code, routeResponse.Body.String())
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
		{http.MethodGet, "/_swobu/workspaces/dev/routes/chat", "GET /_swobu/workspaces/{workspace}/routes/{route}"},
		{http.MethodPut, "/_swobu/workspaces/dev/routes/chat", "PUT /_swobu/workspaces/{workspace}/routes/{route}"},
		{http.MethodPost, "/_swobu/workspaces/dev/routes/chat/rename", "POST /_swobu/workspaces/{workspace}/routes/{route}/rename"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, nil)
		_, pattern := mux.Handler(request)
		if pattern != test.pattern {
			t.Errorf("%s %s pattern = %q, want %q", test.method, test.path, pattern, test.pattern)
		}
	}
}

func TestRouteGETThenPUTExactResponseIsNoOp(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o700)
	path := filepath.Join(dir, "swobu.yaml")
	_ = os.WriteFile(path, []byte("schema_version: 1\nworkspaces: {}\n"), 0o600)
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, _ := workspaces.NewService(store)
	handler := NewWorkspaceControlHandler(service)
	create := `{"slug":"dev","initial_route":"chat","target":{"id":"a","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:OPENAI_API_KEY"}}}}`
	assertWorkspaceCommandStatus(t, handler, http.MethodPost, "/_swobu/workspaces", create, http.StatusCreated)
	get := assertWorkspaceCommandStatus(t, handler, http.MethodGet, "/_swobu/workspaces/dev/routes/chat", "", http.StatusOK)
	put := assertWorkspaceCommandStatus(t, handler, http.MethodPut, "/_swobu/workspaces/dev/routes/chat", get, http.StatusOK)
	var committed workspaces.Workspace
	if err := json.Unmarshal([]byte(put), &committed); err != nil {
		t.Fatalf("PUT response is not committed workspace: %v", err)
	}
	if committed.Slug != "dev" || len(committed.Routes) != 1 || committed.Routes[0].Name != "chat" {
		t.Fatalf("committed workspace = %#v", committed)
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

func TestWorkspaceControlHandlerRejectsEveryRemovedTargetMutationPath(t *testing.T) {
	handler := NewWorkspaceControlHandler(workspaces.Service{})
	for name, request := range map[string]*http.Request{
		"create":     httptest.NewRequest(http.MethodPost, "/_swobu/workspaces/dev/routes/chat/targets", nil),
		"update":     httptest.NewRequest(http.MethodPut, "/_swobu/workspaces/dev/routes/chat/targets/a", nil),
		"delete":     httptest.NewRequest(http.MethodDelete, "/_swobu/workspaces/dev/routes/chat/targets/a", nil),
		"credential": httptest.NewRequest(http.MethodPost, "/_swobu/workspaces/dev/routes/chat/targets/a/credential", nil),
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d body=%s, want removed path 404", response.Code, response.Body.String())
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
		"openrouter": `{"id":"openrouter","model":"openai/gpt-5","protocol":"chat_completions","connection":{"openrouter":{"credential":"secret:openrouter/default"}}}`,
		"chatgpt":    `{"id":"chatgpt","model":"gpt-5","connection":{"chatgpt":{"credential":"secretfile:chatgpt/default"}}}`,
		"ollama":     `{"id":"ollama","model":"llama","protocol":"chat_completions","connection":{"ollama":{}}}`,
		"lmstudio":   `{"id":"lm-studio","model":"local-model","protocol":"responses","connection":{"lmstudio":{"credential":"env:LM_API_TOKEN"}}}`,
		"vllm":       `{"id":"vllm","model":"served-model","protocol":"responses","connection":{"vllm":{"credential":"env:VLLM_API_KEY"}}}`,
		"azure":      `{"id":"azure","model":"deployment","protocol":"responses","connection":{"azure":{"project_endpoint":"https://example.services.ai.azure.com/api/projects/prod","credential":"env:AZURE_KEY"}}}`,
		"bedrock":    `{"id":"bedrock","model":"openai.gpt","protocol":"responses","connection":{"bedrock":{"region":"eu-west-2","endpoint":"https://bedrock-mantle.eu-west-2.api.aws/openai/v1"}}}`,
		"custom":     `{"id":"custom","model":"local","protocol":"chat_completions","connection":{"custom":{"base_url":"https://example.test/v1","header":{"name":"Authorization","credential":"env:CUSTOM_KEY"}}}}`,
	}
	for route, target := range routes {
		body := `{"name":"` + route + `","target":` + target + `}`
		assertWorkspaceCommandStatus(t, handler, http.MethodPost, "/_swobu/workspaces/dev/routes", body, http.StatusOK)
	}

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
	for _, provider := range []string{"openai", "anthropic", "openrouter", "chatgpt", "ollama", "lmstudio", "vllm", "azure", "bedrock", "custom"} {
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
