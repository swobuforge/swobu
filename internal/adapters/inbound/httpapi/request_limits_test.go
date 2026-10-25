package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestPrincipalOperationLimitsAtLocalIngress(t *testing.T) {
	store, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := routing.ParseWorkspaceSlug("engineering")
	principal, first, err := store.Create("build-agent", workspace)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.IssueCredential(principal.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	rev, _ := store.PermissionRevision(principal.ID, workspace)
	services := []routing.ServiceKind{routing.ServiceModels, routing.ServiceMCP}
	rates := []accessstate.RequestRateLimit{{Service: routing.ServiceModels, RequestsPerMinute: 1}, {Service: routing.ServiceMCP, RequestsPerMinute: 1}}
	_, err = store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, workspace, rev, "limit", func() error { return nil }, services, rates)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := NewLocalAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "" {
			t.Error("bearer survived ingress")
		}
		w.WriteHeader(http.StatusNoContent)
	}), store, nil)
	send := func(method, path, token string) (int, http.Header) {
		t.Helper()
		request := httptest.NewRequest(method, path, nil)
		request.RemoteAddr = "127.0.0.1:49265"
		request.Host = "127.0.0.1:7926"
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code, recorder.Header()
	}
	// Metadata reads do not consume downstream operation budget.
	if code, _ := send(http.MethodGet, "/workspaces/engineering/v1/models", first.Token); code != 204 {
		t.Fatal(code)
	}
	if code, _ := send(http.MethodPost, "/workspaces/engineering/v1/responses", first.Token); code != 204 {
		t.Fatal(code)
	}
	if code, headers := send(http.MethodPost, "/workspaces/engineering/v1/responses", other.Token); code != 429 || headers.Get("Retry-After") == "" || headers.Get("Cache-Control") != "no-store" {
		t.Fatalf("quota denial: %d %#v", code, headers)
	}
	// A fabricated Upgrade header on HTTP POST must not bypass metering.
	fakeUpgrade := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7926/workspaces/engineering/v1/responses", nil)
	fakeUpgrade.RemoteAddr = "127.0.0.1:59265"
	fakeUpgrade.Header.Set("Authorization", "Bearer "+other.Token)
	fakeUpgrade.Header.Set("Connection", "Upgrade")
	fakeUpgrade.Header.Set("Upgrade", "websocket")
	fakeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(fakeRecorder, fakeUpgrade)
	if fakeRecorder.Code != 429 {
		t.Fatalf("fabricated Upgrade header bypassed rate limit: %d", fakeRecorder.Code)
	}
	if code, _ := send(http.MethodPost, "/workspaces/engineering/mcp", other.Token); code != 204 {
		t.Fatalf("unrelated MCP denied: %d", code)
	}
	if code, _ := send(http.MethodPost, "/workspaces/engineering/mcp", first.Token); code != 429 {
		t.Fatalf("MCP shared quota: %d", code)
	}
	if code, _ := send(http.MethodPost, "/workspaces/engineering/search", other.Token); code != 403 {
		t.Fatalf("ungranted Search admitted: %d", code)
	}
	if calls != 3 {
		t.Fatalf("upstream was reached %d times, expected 3", calls)
	}
}

func TestLocalNativeNotImpersonatedByPrincipalQuota(t *testing.T) {
	store, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewLocalAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), store, nil)
	for i := 0; i < 8; i++ {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7926/workspaces/engineering/v1/responses", nil)
		req.RemoteAddr = "127.0.0.1:59265"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != 204 {
			t.Fatalf("local host request denied by another Principal's ceiling: %d", rec.Code)
		}
	}
}

func TestPermissionRateWireRejectsMalformedPolicyWithoutPublication(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "rate-wire", "prod")
	if err != nil {
		t.Fatal(err)
	}
	revision := permissionTestRevision(t, handler, created.Access.ID)
	for name, limits := range map[string]string{
		"null":              "null",
		"unknown member":    `[{"service":"models","requests_per_minute":1,"tokens":1}]`,
		"fractional":        `[{"service":"models","requests_per_minute":1.5}]`,
		"negative":          `[{"service":"models","requests_per_minute":-1}]`,
		"zero":              `[{"service":"models","requests_per_minute":0}]`,
		"too large":         `[{"service":"models","requests_per_minute":100001}]`,
		"unknown Service":   `[{"service":"other","requests_per_minute":1}]`,
		"duplicate Service": `[{"service":"models","requests_per_minute":1},{"service":"models","requests_per_minute":2}]`,
		"outside scope":     `[{"service":"mcp","requests_per_minute":1}]`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, query := range []string{"", "?dry_run=true"} {
				req := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/prod/permissions/"+created.Access.ID+query, strings.NewReader(`{"action":"use","services":["models"],"request_limits":`+limits+`}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("If-Match", `"`+revision+`"`)
				req.Header.Set("Idempotency-Key", "rejected-rate-wire")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, permissionOperatorRequest(req))
				if response.Code != 400 {
					t.Fatalf("policy accepted: %d %s", response.Code, response.Body.String())
				}
				if permissionTestRevision(t, handler, created.Access.ID) != revision {
					t.Fatal("rejected draft changed authority")
				}
			}
		})
	}
}
