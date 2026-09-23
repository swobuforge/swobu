package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/accessstate"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
)

func accessHandler(t *testing.T) AccessControlHandler {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	configStore, err := configstore.OpenOrCreate(filepath.Join(dir, "swobu.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceService, err := workspaces.NewService(configStore)
	if err != nil {
		t.Fatal(err)
	}
	_, err = workspaceService.CreateWorkspace(t.Context(), workspaces.CreateWorkspace{Slug: "prod", InitialRoute: "chat", Target: workspaces.TargetDraft{ID: "tgt_access", Model: "gpt-5", Protocol: "responses", Connection: workspaces.StandardConnection("openai", "", "env:OPENAI_API_KEY")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = configStore.Close() })
	state, _ := accessstate.Open(filepath.Join(t.TempDir(), "access.json"))
	service, err := accessapp.NewService(state, configStore, &routebindings.Coordinator{})
	if err != nil {
		t.Fatal(err)
	}
	return NewAccessControlHandler(service)
}

func TestAccessControlOneShotCreateAndBuiltinList(t *testing.T) {
	handler := accessHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/_swobu/access", strings.NewReader(`{"name":"CI","workspace":"prod"}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status/cache=%d/%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	var created accessapp.IssueResult
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, "swak_") {
		t.Fatalf("token=%q", created.Token)
	}
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/_swobu/access", nil))
	if !strings.Contains(list.Body.String(), `"id":"local"`) || strings.Contains(list.Body.String(), created.Token) {
		t.Fatalf("list=%s", list.Body.String())
	}
}

func TestAccessControlRequiresJSONMutationContentType(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/_swobu/access", strings.NewReader(`{"name":"CI","workspace":"prod"}`))
	response := httptest.NewRecorder()
	accessHandler(t).ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d", response.Code)
	}
}
