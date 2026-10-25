package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestWorkspaceServiceLimitsHTTPUsesWorkspaceAuthorityAndRevision(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.OpenOrCreate(filepath.Join(dir, "swobu.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, _ := workspaces.NewService(store, workspaces.Dependencies{})
	initial, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "engineering"}, "fixture", "create-1", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeControlAPIHandler(service)
	policy := routing.ServiceLimits{Models: routing.ServiceQuota{RequestsPerMinute: 60, Concurrent: 2}, MCP: routing.ServiceQuota{RequestsPerMinute: 12}, Search: routing.ServiceQuota{RequestsPerMinute: 30}}
	raw, _ := json.Marshal(policy)
	request := func(method, etag string, local bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/workspaces/engineering/service-limits", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		if local {
			req = permissionOperatorRequest(req)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if got := request(http.MethodPut, initial.ManagementETag, false); got.Code != 403 {
		t.Fatalf("unauthorized=%d", got.Code)
	}
	if got := request(http.MethodPut, "", true); got.Code != 428 {
		t.Fatalf("missing ETag=%d", got.Code)
	}
	got := request(http.MethodPut, initial.ManagementETag, true)
	if got.Code != 200 {
		t.Fatalf("apply=%d %s", got.Code, got.Body.String())
	}
	invalid := routing.ServiceLimits{MCP: routing.ServiceQuota{Concurrent: 1}}
	invalidRaw, _ := json.Marshal(invalid)
	bad := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/engineering/service-limits", bytes.NewReader(invalidRaw))
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("If-Match", got.Header().Get("ETag"))
	bad = permissionOperatorRequest(bad)
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, bad)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid limits were not rejected as a client error: %d %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	if stale := request(http.MethodPut, initial.ManagementETag, true); stale.Code != 412 {
		t.Fatalf("stale=%d", stale.Code)
	}
	read := request(http.MethodGet, "", true)
	if read.Code != 200 {
		t.Fatalf("read=%d", read.Code)
	}
	var restored routing.ServiceLimits
	if err := json.Unmarshal(read.Body.Bytes(), &restored); err != nil || restored != policy {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
}
