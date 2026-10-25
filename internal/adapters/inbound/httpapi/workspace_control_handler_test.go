package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestWorkspaceControlHandlerCreatesAndReadsSemanticResources(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(dir, 0o700)
	path := filepath.Join(dir, "swobu.yaml")
	_ = os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600)
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeControlAPIHandler(service)
	body := `{"slug":"dev","initial_route":"chat","target":{"id":"a","model":"gpt-5","protocol":"responses","connection_id":"cn_openai"}}`
	request := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", strings.NewReader(body)))
	request.Header.Set("Idempotency-Key", "semantic-resource-create")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	NewNodeControlAPIHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"default_route"`) || !strings.Contains(response.Body.String(), `"name":"chat"`) {
		t.Fatalf("create response=%s", response.Body.String())
	}
	route := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/dev/model-routes/chat", nil)
	routeResponse := httptest.NewRecorder()
	handler.ServeHTTP(routeResponse, route)
	if routeResponse.Code != http.StatusOK || !strings.Contains(routeResponse.Body.String(), `"connection_id":"cn_openai"`) {
		t.Fatalf("route status=%d body=%s", routeResponse.Code, routeResponse.Body.String())
	}
}

func TestWorkspaceControlHandlerCreatesEmptyWorkspaceWithoutFabricatedRoute(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	_ = os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600)
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", strings.NewReader(`{"slug":"research"}`))
	req = permissionOperatorRequest(req)
	req.Header.Set("Idempotency-Key", "empty-workspace")
	req.Header.Set("Content-Type", "application/json")
	NewNodeControlAPIHandler(service).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated || strings.Contains(recorder.Body.String(), `"default_route"`) || !strings.Contains(recorder.Body.String(), `"routes":[]`) {
		t.Fatalf("status/body=%d/%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"readiness":{"llm":"disabled","mcp":"disabled"}`) {
		t.Fatalf("readiness projection=%s", recorder.Body.String())
	}
	if _, ok := store.Config().Workspace(mustWorkspaceSlug(t, "research")); !ok {
		t.Fatal("empty workspace was not persisted")
	}
}

func mustWorkspaceSlug(t *testing.T, raw string) routing.WorkspaceSlug {
	t.Helper()
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		t.Fatal(err)
	}
	return slug
}

func TestWorkspaceControlRouterUsesStandardGETHeadSemantics(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o700)
	path := filepath.Join(dir, "swobu.yaml")
	_ = os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600)
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeControlAPIHandler(service)
	request := httptest.NewRequest(http.MethodHead, "/api/v1/workspaces", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want standard GET-pattern match", response.Code)
	}
}

func TestWorkspaceControlRouterRegistersOnlyCanonicalCommands(t *testing.T) {
	mux := NewNodeControlAPIHandler(workspaces.Service{})
	for _, tc := range []struct{ method, path, pattern string }{
		{http.MethodGet, "/api/v1/workspaces", "GET /api/v1/workspaces"},
		{http.MethodGet, "/api/v1/workspaces/dev", "GET /api/v1/workspaces/{workspace}"},
		{http.MethodGet, "/api/v1/workspaces/dev/model-routes/chat", "GET /api/v1/workspaces/{workspace}/model-routes/{route}"},
		{http.MethodPut, "/api/v1/workspaces/dev/model-routes/chat", "PUT /api/v1/workspaces/{workspace}/model-routes/{route}"},
		{http.MethodDelete, "/api/v1/workspaces/dev/model-routes/chat", "DELETE /api/v1/workspaces/{workspace}/model-routes/{route}"},
	} {
		_, pattern := mux.Handler(httptest.NewRequest(tc.method, tc.path, nil))
		if pattern != tc.pattern {
			t.Errorf("%s %s matched %q, want %q", tc.method, tc.path, pattern, tc.pattern)
		}
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/_swobu/workspaces"},
		{http.MethodPost, "/_swobu/workspaces/dev/routes"},
		{http.MethodGet, "/api/v1/workspaces/dev/routes/chat"},
		{http.MethodPut, "/api/v1/workspaces/dev/routes/chat"},
		{http.MethodDelete, "/api/v1/workspaces/dev/routes/chat"},
	} {
		if _, pattern := mux.Handler(httptest.NewRequest(tc.method, tc.path, nil)); pattern != "" {
			t.Errorf("retired %s %s matched %q", tc.method, tc.path, pattern)
		}
	}
}

func TestLegacyWorkspaceDeletionIsNotRegistered(t *testing.T) {
	handler := NewNodeControlAPIHandler(workspaces.Service{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/_swobu/workspaces/dev", strings.NewReader("{}")))
	if response.Code != http.StatusNotFound {
		t.Fatalf("legacy deletion status = %d, want 404", response.Code)
	}
}

func TestWorkspaceAttachmentsUseOnlyCanonicalNodeControl(t *testing.T) {
	legacy := NewNodeControlAPIHandler(workspaces.Service{})
	for _, testCase := range []struct{ method, path string }{
		{http.MethodPut, "/_swobu/workspaces/dev/mcp-sources/github"},
		{http.MethodDelete, "/_swobu/workspaces/dev/mcp-sources/github"},
		{http.MethodPut, "/_swobu/workspaces/dev/search-source"},
		{http.MethodDelete, "/_swobu/workspaces/dev/search-source"},
	} {
		response := httptest.NewRecorder()
		legacy.ServeHTTP(response, httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(`{"source_id":"fixture"}`)))
		if response.Code != http.StatusNotFound {
			t.Errorf("legacy %s %s status = %d, want 404", testCase.method, testCase.path, response.Code)
		}
	}

	canonical := NewNodeControlAPIHandler(workspaces.Service{})
	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/workspaces/dev/mcp-sources/github"},
		{http.MethodDelete, "/api/v1/workspaces/dev/mcp-sources/github"},
		{http.MethodPut, "/api/v1/workspaces/dev/search-source"},
		{http.MethodDelete, "/api/v1/workspaces/dev/search-source"},
	} {
		response := httptest.NewRecorder()
		canonical.ServeHTTP(response, permissionOperatorRequest(httptest.NewRequest(tc.method, tc.path, nil)))
		if response.Code != http.StatusNotFound {
			t.Errorf("retired %s %s = %d, want 404", tc.method, tc.path, response.Code)
		}
	}
	for _, testCase := range []struct{ method, path, pattern string }{
		{http.MethodGet, "/api/v1/workspaces/dev/mcp-mounts", "GET /api/v1/workspaces/{workspace}/mcp-mounts"},
		{http.MethodGet, "/api/v1/workspaces/dev/mcp-mounts/github", "GET /api/v1/workspaces/{workspace}/mcp-mounts/{alias}"},
		{http.MethodPut, "/api/v1/workspaces/dev/mcp-mounts/github", "PUT /api/v1/workspaces/{workspace}/mcp-mounts/{alias}"},
		{http.MethodDelete, "/api/v1/workspaces/dev/mcp-mounts/github", "DELETE /api/v1/workspaces/{workspace}/mcp-mounts/{alias}"},
		{http.MethodPut, "/api/v1/workspaces/dev/search-route", "PUT /api/v1/workspaces/{workspace}/search-route"},
		{http.MethodDelete, "/api/v1/workspaces/dev/search-route", "DELETE /api/v1/workspaces/{workspace}/search-route"},
		{http.MethodGet, "/api/v1/workspaces/dev/search-route", "GET /api/v1/workspaces/{workspace}/search-route"},
	} {
		_, pattern := canonical.Handler(httptest.NewRequest(testCase.method, testCase.path, nil))
		if pattern != testCase.pattern {
			t.Errorf("canonical %s %s pattern = %q, want %q", testCase.method, testCase.path, pattern, testCase.pattern)
		}
	}
}

// Node Control must not offer a second, unreviewed route mutation path. The
// old local shell remains isolated until the launcher/terminal deletion card.
func TestNodeControlRoutesHaveOnlyRevisionCheckedMutations(t *testing.T) {
	canonical := NewNodeControlAPIHandler(workspaces.Service{})
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/workspaces/dev/routes"},
		{http.MethodDelete, "/api/v1/workspaces/dev/routes/chat"},
		{http.MethodPut, "/api/v1/workspaces/dev/routes/chat"},
		{http.MethodPost, "/api/v1/workspaces/dev/routes/chat/rename"},
	} {
		_, pattern := canonical.Handler(httptest.NewRequest(tc.method, tc.path, nil))
		if pattern != "" {
			t.Errorf("unreviewed Node Control route %s %s still registered as %q", tc.method, tc.path, pattern)
		}
	}
	for _, tc := range []struct{ method, path, want string }{
		{http.MethodPut, "/api/v1/workspaces/dev/model-routes/chat", "PUT /api/v1/workspaces/{workspace}/model-routes/{route}"},
		{http.MethodDelete, "/api/v1/workspaces/dev/model-routes/chat", "DELETE /api/v1/workspaces/{workspace}/model-routes/{route}"},
	} {
		_, pattern := canonical.Handler(httptest.NewRequest(tc.method, tc.path, nil))
		if pattern != tc.want {
			t.Errorf("retained checked route %s %s = %q, want %q", tc.method, tc.path, pattern, tc.want)
		}
	}
}

func TestCanonicalConnectionMountRejectsMissingImportedIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.OpenOrCreate(filepath.Join(dir, "swobu.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, err := connectionfixture.Open(filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workspaces.NewService(store, workspaces.Dependencies{Connections: registry})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeControlAPIHandler(service)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/dev/mcp-mounts/docs?dry_run=true", strings.NewReader(`{"connection_id":"docs","selected_tools":["search"]}`))
	request = permissionOperatorRequest(request)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "Connection") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
	// The retired field is refused by the canonical DTO before any storage or
	// revision machinery is consulted. A bad connection_id is tested at the
	// application boundary with a configured registry below.
	search := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/dev/search-route", strings.NewReader(`{"source_id":"search"}`))
	search = permissionOperatorRequest(search)
	search.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, search)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "source_id") {
		t.Fatalf("search status=%d body=%s", response.Code, response.Body)
	}
	mount := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/dev/mcp-mounts/docs?dry_run=true", strings.NewReader(`{"connection_id":"cn_mcp","selected_tools":[],"integration":"old-source"}`))
	mount = permissionOperatorRequest(mount)
	mount.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, mount)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "retired") {
		t.Fatalf("mount status=%d body=%s", response.Code, response.Body)
	}
}

func TestCanonicalAttachmentsRejectRetiredFieldsEvenWhenEmpty(t *testing.T) {
	handler := NewNodeControlAPIHandler(workspaces.Service{})
	for _, test := range []struct {
		method, path, body string
	}{
		{http.MethodPut, "/mcp-mounts/docs?dry_run=true", `{"connection_id":"cn_docs","selected_tools":[],"source_id":"x"}`},
		{http.MethodPut, "/mcp-mounts/docs?dry_run=true", `{"connection_id":"cn_docs","selected_tools":[],"source_id":""}`},
		{http.MethodPut, "/mcp-mounts/docs?dry_run=true", `{"connection_id":"cn_docs","selected_tools":[],"source_id":null}`},
		{http.MethodPut, "/search-route", `{"connection_id":"cn_search","source_id":null}`},
		{http.MethodPut, "/search-route", `{"connection_id":"cn_search","source_id":""}`},
		{http.MethodPut, "/mcp-mounts/docs?dry_run=true", `{"connection_id":"cn_docs","selected_tools":[],"integration":null}`},
		{http.MethodPut, "/mcp-mounts/docs?dry_run=true", `{"connection_id":"cn_docs","selected_tools":[],"allow_tools":null}`},
	} {
		t.Run(test.method+test.path+test.body, func(t *testing.T) {
			request := permissionOperatorRequest(httptest.NewRequest(test.method, "/api/v1/workspaces/dev"+test.path, strings.NewReader(test.body)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("retired field status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestNodeControlAPIUsesVersionedPathsWithoutLegacyAlias(t *testing.T) {
	mux := NewNodeControlAPIHandler(workspaces.Service{})
	paths := []struct {
		method, path, want string
	}{
		{http.MethodGet, "/api/v1/workspaces", "GET /api/v1/workspaces"},
		{http.MethodPost, "/api/v1/workspaces", "POST /api/v1/workspaces"},
		{http.MethodGet, "/api/v1/workspaces/example", "GET /api/v1/workspaces/{workspace}"},
		{http.MethodDelete, "/api/v1/workspaces/example", "DELETE /api/v1/workspaces/{workspace}"},
		{http.MethodGet, "/api/v1/workspaces/example/model-routes/chat", "GET /api/v1/workspaces/{workspace}/model-routes/{route}"},
		{http.MethodPut, "/api/v1/workspaces/example/model-routes/chat", "PUT /api/v1/workspaces/{workspace}/model-routes/{route}"},
	}
	for _, path := range paths {
		request := httptest.NewRequest(path.method, path.path, nil)
		_, pattern := mux.Handler(request)
		if pattern != path.want {
			t.Errorf("%s %s pattern = %q, want %q", path.method, path.path, pattern, path.want)
		}
	}
	legacy := httptest.NewRequest(http.MethodGet, "/_swobu/workspaces/example", nil)
	if _, pattern := mux.Handler(legacy); pattern != "" {
		t.Fatalf("legacy path unexpectedly reachable through Node Control API: %q", pattern)
	}
}

func TestRouteGETThenPUTExactResponseIsNoOp(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0o700)
	path := filepath.Join(dir, "swobu.yaml")
	_ = os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600)
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, _ := workspaces.NewService(store, workspaces.Dependencies{})
	handler := NewNodeControlAPIHandler(service)
	create := `{"slug":"dev","initial_route":"chat","target":{"id":"a","model":"gpt-5","protocol":"responses","connection_id":"cn_openai"}}`
	assertWorkspaceCommandStatus(t, NewNodeControlAPIHandler(service), http.MethodPost, "/api/v1/workspaces", create, http.StatusCreated)
	get := assertWorkspaceCommandStatus(t, handler, http.MethodGet, "/api/v1/workspaces/dev/model-routes/chat", "", http.StatusOK)
	inspected, err := service.GetWorkspace(t.Context(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	request := permissionOperatorRequest(httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/dev/model-routes/chat", strings.NewReader(get)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"`+inspected.ManagementETag+`"`)
	response := httptest.NewRecorder()
	NewNodeControlAPIHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != request.Header.Get("If-Match") || strings.Contains(response.Body.String(), `"replayed"`) {
		t.Fatalf("no-op: %d %s", response.Code, response.Body.String())
	}
	put := response.Body.String()
	var committed workspaces.Workspace
	if err := json.Unmarshal([]byte(put), &committed); err != nil {
		t.Fatalf("PUT response is not committed workspace: %v", err)
	}
	if committed.Slug != "dev" || len(committed.Routes) != 1 || committed.Routes[0].Name != "chat" {
		t.Fatalf("committed workspace = %#v", committed)
	}
}

func TestWorkspaceControlHandlerRejectsWholeWorkspaceReplacement(t *testing.T) {
	handler := NewNodeControlAPIHandler(workspaces.Service{})
	request := httptest.NewRequest(http.MethodPut, "/_swobu/workspaces/dev", strings.NewReader(`{"routes":[]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("retired Workspace replacement status = %d, want 404", response.Code)
	}
}

func TestWorkspaceControlHandlerRejectsMalformedTrailingJSON(t *testing.T) {
	handler := NewNodeControlAPIHandler(workspaces.Service{})
	body := `{"slug":"dev","initial_route":"chat","target":{"id":"a","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:KEY"}}}} garbage`
	request := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", strings.NewReader(body)))
	request.Header.Set("Idempotency-Key", "malformed-create")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestWorkspaceControlHandlerRejectsEveryRemovedTargetMutationPath(t *testing.T) {
	handler := NewNodeControlAPIHandler(workspaces.Service{})
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

func TestWorkspaceCommandsRoundTripConnectionReferencesAcrossStoreReopen(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeControlAPIHandler(service)

	create := `{"slug":"dev","initial_route":"chat","target":{"id":"openai","model":"gpt-5","protocol":"responses","connection_id":"cn_openai"}}`
	assertWorkspaceCommandStatus(t, NewNodeControlAPIHandler(service), http.MethodPost, "/api/v1/workspaces", create, http.StatusCreated)

	routes := map[string]string{
		"anthropic":  `{"id":"anthropic","model":"claude","protocol":"messages","connection_id":"cn_anthropic"}`,
		"openrouter": `{"id":"openrouter","model":"openai/gpt-5","protocol":"chat_completions","connection_id":"cn_openrouter"}`,
		"chatgpt":    `{"id":"chatgpt","model":"gpt-5","connection_id":"cn_chatgpt","protocol":"responses"}`,
		"ollama":     `{"id":"ollama","model":"llama","protocol":"chat_completions","connection_id":"cn_ollama"}`,
		"lmstudio":   `{"id":"lm-studio","model":"local-model","protocol":"responses","connection_id":"cn_lmstudio"}`,
		"vllm":       `{"id":"vllm","model":"served-model","protocol":"responses","connection_id":"cn_vllm"}`,
		"azure":      `{"id":"azure","model":"deployment","protocol":"responses","connection_id":"cn_azure"}`,
		"bedrock":    `{"id":"bedrock","model":"openai.gpt","protocol":"responses","connection_id":"cn_bedrock"}`,
		"custom":     `{"id":"custom","model":"local","protocol":"chat_completions","connection_id":"cn_custom"}`,
	}
	for route, target := range routes {
		value, err := service.GetWorkspace(t.Context(), "dev")
		if err != nil {
			t.Fatal(err)
		}
		body := `{"tiers":[{"targets":[` + target + `]}]}`
		req := permissionOperatorRequest(httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/dev/model-routes/"+route, strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+value.ManagementETag+`"`)
		req.Header.Set("Idempotency-Key", "route-"+route)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		if result.Code != http.StatusOK {
			t.Fatalf("create %s status=%d body=%s", route, result.Code, result.Body.String())
		}
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopenedService, err := workspaces.NewService(reopened, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	reopenedHandler := NewNodeControlAPIHandler(reopenedService)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/dev", nil)
	response := httptest.NewRecorder()
	reopenedHandler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("reopen GET status=%d body=%s", response.Code, response.Body.String())
	}
	for _, provider := range []string{"openai", "anthropic", "openrouter", "chatgpt", "ollama", "lmstudio", "vllm", "azure", "bedrock", "custom"} {
		if !strings.Contains(response.Body.String(), `"connection_id":"cn_`+provider+`"`) {
			t.Errorf("reopened response missing Connection %q: %s", provider, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), `"credential":`) || strings.Contains(response.Body.String(), `"connection":`) {
		t.Fatal("Workspace readback exposed embedded upstream configuration")
	}
}

func assertWorkspaceCommandStatus(t *testing.T, handler http.Handler, method, path, body string, want int) string {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == http.MethodPost && path == "/api/v1/workspaces" {
		request = permissionOperatorRequest(request)
		request.Header.Set("Idempotency-Key", "fixture-creation")
	}
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.Code, want, response.Body.String())
	}
	return response.Body.String()
}

func TestModelRoutePreviewAndRevisionCheckedOrderHTTP(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Update(context.Background(), func(cfg routing.Config) (routing.Config, error) {
		slug, _ := routing.ParseWorkspaceSlug("dev")
		chat, _ := routing.ParseRouteName("chat")
		return cfg.CreateWorkspace(routing.WorkspaceSeed{Slug: slug, Route: chat, Target: workspaceControlTestTarget(t, "chat-target")})
	}); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	pattern, _ := routing.ParseRouteName("deepseek/*")
	if _, err := store.Update(context.Background(), func(cfg routing.Config) (routing.Config, error) {
		return cfg.CreateRoute(slug, pattern, workspaceControlTestTarget(t, "family-target"))
	}); err != nil {
		t.Fatal(err)
	}
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeControlAPIHandler(service)
	prior, _ := store.Config().Workspace(slug)
	etag := fmt.Sprintf(`"%s-%d"`, prior.ManagementIncarnation(), prior.ManagementRevision())
	preview := func(model string) string {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/workspaces/dev/model-match?model="+model, nil))
		if response.Code != 200 {
			t.Fatalf("preview %s: %d %s", model, response.Code, response.Body.String())
		}
		return response.Body.String()
	}
	if !strings.Contains(preview("deepseek%2Fchat"), `"pattern":"deepseek/*"`) {
		t.Fatal("namespaced glob did not match")
	}
	if !strings.Contains(preview("unknown%2Fmodel"), `"matched":false`) {
		t.Fatal("glob crossed / separator")
	}
	body := `{"order":["deepseek/*","chat"]}`
	request := permissionOperatorRequest(httptest.NewRequest("PATCH", "/api/v1/workspaces/dev/model-routes", strings.NewReader(body)))
	request.Header.Set("If-Match", etag)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("reorder: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"name":"deepseek/*"`) {
		t.Fatal("projection lost glob pattern")
	}
	stale := permissionOperatorRequest(httptest.NewRequest("PATCH", "/api/v1/workspaces/dev/model-routes", strings.NewReader(body)))
	stale.Header.Set("If-Match", etag)
	stale.Header.Set("Content-Type", "application/json")
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != 412 {
		t.Fatalf("stale reorder: %d %s", staleResponse.Code, staleResponse.Body.String())
	}
	encodedRoute := httptest.NewRecorder()
	handler.ServeHTTP(encodedRoute, httptest.NewRequest("GET", "/api/v1/workspaces/dev/model-routes/deepseek%2F*", nil))
	if encodedRoute.Code != 200 {
		t.Fatalf("encoded route identity: %d %s", encodedRoute.Code, encodedRoute.Body.String())
	}
}

// Fixture target uses the ordinary routing constructor without provisioning a
// real Connection, as the control API only publishes redacted references.
func workspaceControlTestTarget(t *testing.T, id string) routing.Target {
	t.Helper()
	targetID, _ := routing.ParseTargetID(id)
	model, _ := routing.ParseUpstreamModel("gpt-5")
	provider, _ := routing.ParseProvider("openai", func(s string) bool { return s == "openai" })
	protocol, _ := routing.ParseProtocol("responses", provider, func(routing.Provider, string) bool { return true })
	connection, _ := connectionidentity.Parse("cn_shared")
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
