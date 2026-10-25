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

func TestLegacyWorkspaceCreationIsAbsentAndCannotPublish(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "routing.yaml")
	store, err := configstore.OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"", "?dry_run=true", "?dry_run=false", "?unknown=true"} {
		request := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, "/_swobu/workspaces"+query, strings.NewReader(`{"slug":"legacy-created"}`)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "original-key")
		response := httptest.NewRecorder()
		NewNodeControlAPIHandler(service).ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("legacy creation status=%d", response.Code)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) || len(store.Config().Workspaces()) != 0 {
		t.Fatal("retired legacy creation published state", err)
	}
}

func TestCanonicalWorkspaceCreationRequiresKeyAndReplaysOriginalResult(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "routing.yaml")
	store, err := configstore.OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeControlAPIHandler(service)
	post := func(body, key string, trusted bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", strings.NewReader(body))
		if trusted {
			req = permissionOperatorRequest(req)
		}
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if key == "duplicate" {
			req.Header.Add("Idempotency-Key", "second")
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	body := `{"slug":"research","display_name":"Research"}`
	before, _ := os.ReadFile(path)
	previewRequest := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, "/api/v1/workspaces?dry_run=true", strings.NewReader(body)))
	previewRequest.Header.Set("Content-Type", "application/json")
	preview := httptest.NewRecorder()
	handler.ServeHTTP(preview, previewRequest)
	afterPreview, _ := os.ReadFile(path)
	var proposed workspaces.CreationPreview
	if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &proposed) != nil || proposed.Slug != "research" || proposed.DisplayName != "Research" || proposed.RouteCount != 0 || proposed.TargetCount != 0 || proposed.Readiness.LLM != "disabled" || preview.Header().Get("ETag") != "" || preview.Header().Get("Cache-Control") != "no-store" || string(before) != string(afterPreview) || len(store.Config().Workspaces()) != 0 {
		t.Fatal("creation preview failed or published", preview.Code)
	}
	for _, probe := range []struct {
		key     string
		trusted bool
		status  int
	}{{"", true, 400}, {"bad key", true, 400}, {"duplicate", true, 400}, {strings.Repeat("k", 257), true, 400}, {"key", false, 403}} {
		out := post(body, probe.key, probe.trusted)
		after, _ := os.ReadFile(path)
		if out.Code != probe.status || string(before) != string(after) {
			t.Fatalf("creation admission status=%d expected=%d", out.Code, probe.status)
		}
	}
	original := post(body, "key", true)
	if original.Code != 201 {
		t.Fatalf("create status=%d", original.Code)
	}
	var created workspaces.Workspace
	if json.Unmarshal(original.Body.Bytes(), &created) != nil || created.ManagementETag == "" {
		t.Fatal("invalid original result")
	}
	changed := post(`{"slug":"research","display_name":"Changed"}`, "key", true)
	if changed.Code != 409 {
		t.Fatalf("changed payload status=%d", changed.Code)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err = workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	handler = NewNodeControlAPIHandler(service)
	before, _ = os.ReadFile(path)
	replay := post(body, "key", true)
	after, _ := os.ReadFile(path)
	if replay.Code != 200 || replay.Body.String() != original.Body.String() || replay.Header().Get("ETag") != original.Header().Get("ETag") || replay.Header().Get("Location") != "/api/v1/workspaces/research" || string(before) != string(after) {
		t.Fatal("replay changed result or published")
	}
}
