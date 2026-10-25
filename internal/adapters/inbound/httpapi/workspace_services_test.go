package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestWorkspaceServicesHTTPAdmitsCheckedEmptyLifecycle(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "swobu.yaml")
	store, err := configstore.OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "research"}, "fixture-operator", "services-creation", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeControlAPIHandler(service)
	get := func() []workspaces.ServiceState {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/workspaces/research/services", nil))
		if res.Code != 200 {
			t.Fatalf("GET services: %d %s", res.Code, res.Body)
		}
		var rows []workspaces.ServiceState
		if err := json.Unmarshal(res.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if rows := get(); len(rows) != 3 || rows[0].Enabled || rows[1].Enabled || rows[2].Enabled {
		t.Fatalf("empty Workspace: %+v", rows)
	}
	change := func(method, kind, etag string, trusted bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/workspaces/research/services/"+kind, nil)
		req.Header.Set("If-Match", `"`+etag+`"`)
		if trusted {
			req = permissionOperatorRequest(req)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	if got := change(http.MethodPut, "mcp", created.ManagementETag, false); got.Code != 403 {
		t.Fatalf("remote mutation: %d", got.Code)
	}
	got := change(http.MethodPut, "mcp", created.ManagementETag, true)
	if got.Code != 200 {
		t.Fatalf("enable MCP: %d %s", got.Code, got.Body.String())
	}
	var enabled workspaces.Workspace
	if err := json.Unmarshal(got.Body.Bytes(), &enabled); err != nil {
		t.Fatal(err)
	}
	if len(enabled.EnabledServices) != 1 || enabled.EnabledServices[0] != "mcp" || enabled.Readiness.MCP != "not_configured" {
		t.Fatalf("enabled-empty response: %+v", enabled)
	}
	if change(http.MethodDelete, "mcp", created.ManagementETag, true).Code != 412 {
		t.Fatal("stale ETag permitted")
	}
	if change(http.MethodPut, "images", enabled.ManagementETag, true).Code != 400 {
		t.Fatal("unreleased Service accepted")
	}
	if change(http.MethodDelete, "mcp", enabled.ManagementETag, true).Code != 200 {
		t.Fatal("empty MCP cannot disable")
	}
	rows := get()
	if rows[0].Enabled || rows[1].Enabled || rows[2].Enabled {
		t.Fatalf("disable not visible: %+v", rows)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "enabled_services:") {
		t.Fatal("nondeclarative Service state", err)
	}
}

func TestWorkspaceServiceDisableRejectsNonEmptyMCPWithoutMutation(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "swobu.yaml")
	store, err := configstore.OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "tools"}, "operator", "create-tools", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SetServiceChecked(t.Context(), "tools", "mcp", true, created.ManagementETag, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("tools")
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		return c.SetMCPMount(slug, "github", routing.MCPMount{ConnectionID: "cn_github", AllowedTools: []string{"read_issue"}})
	}); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetWorkspace(t.Context(), "tools")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	req := permissionOperatorRequest(httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/tools/services/mcp", nil))
	req.Header.Set("If-Match", `"`+current.ManagementETag+`"`)
	result := httptest.NewRecorder()
	NewNodeControlAPIHandler(service).ServeHTTP(result, req)
	if result.Code != http.StatusConflict {
		t.Fatalf("nonempty service deletion: %d %s", result.Code, result.Body)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("disable refusal changed desired-state bytes")
	}
	unchanged, _ := service.GetWorkspace(t.Context(), "tools")
	if unchanged.ManagementETag != current.ManagementETag || len(unchanged.MCPMounts) != 1 {
		t.Fatal("disable refusal changed published MCP state")
	}
}

// HTTP clients cannot bypass the authority review by omitting a body, forging
// a digest or reusing a pre-grant-change preview. The managed browser and
// trusted local CLI use the same application guard.
func TestWorkspaceServiceActivationHTTPRequiresExactReview(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	store, err := configstore.OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	node, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InstallNodePublisher(node.PublishRouting); err != nil {
		t.Fatal(err)
	}
	access, err := accessstate.OpenInline(nil, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workspaces.NewService(store, workspaces.Dependencies{Access: access})
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "research"}, "operator", "create", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	if _, _, err := access.Create("Broad agent", slug); err != nil {
		t.Fatal(err)
	}
	handler := NewNodeControlAPIHandler(service)
	endpoint := "/api/v1/workspaces/research/services/models"
	request := func(body string) *httptest.ResponseRecorder {
		var payload io.Reader
		if body != "" {
			payload = strings.NewReader(body)
		}
		req := permissionOperatorRequest(httptest.NewRequest(http.MethodPut, endpoint, payload))
		req.Header.Set("If-Match", `"`+current.ManagementETag+`"`)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	if res := request(""); res.Code != http.StatusPreconditionRequired {
		t.Fatalf("unreviewed impact: %d %s", res.Code, res.Body)
	}
	if res := request(`{"review_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`); res.Code != http.StatusConflict {
		t.Fatalf("forged impact: %d %s", res.Code, res.Body)
	}
	if res := request(`{"review_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","ignored":true}`); res.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d %s", res.Code, res.Body)
	}
	if res := request(`{"review_digest":"deadbeef"}`); res.Code != http.StatusBadRequest {
		t.Fatalf("invalid hex envelope: %d %s", res.Code, res.Body)
	}
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/research/services", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("service preview: %d %s", get.Code, get.Body)
	}
	var services []workspaces.ServiceState
	if err := json.Unmarshal(get.Body.Bytes(), &services); err != nil {
		t.Fatal(err)
	}
	if len(services) != 3 || services[0].ActivationReview == nil || !services[0].ActivationReview.RequiresConfirmation || len(services[0].ActivationReview.AffectedPrincipals) != 1 {
		t.Fatalf("review disclosure: %+v", services)
	}
	review := services[0].ActivationReview.Digest
	if _, _, err := access.Create("Late agent", slug); err != nil {
		t.Fatal(err)
	}
	if res := request(`{"review_digest":"` + review + `"}`); res.Code != http.StatusConflict {
		t.Fatalf("stale impact: %d %s", res.Code, res.Body)
	}
	fresh := httptest.NewRecorder()
	handler.ServeHTTP(fresh, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/research/services", nil))
	if err := json.Unmarshal(fresh.Body.Bytes(), &services); err != nil {
		t.Fatal(err)
	}
	review = services[0].ActivationReview.Digest
	if res := request(`{"review_digest":"` + review + `"}`); res.Code != http.StatusOK {
		t.Fatalf("reviewed impact: %d %s", res.Code, res.Body)
	}
}
