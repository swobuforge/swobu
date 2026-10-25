package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/publications"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestPublishedWorkspaceRequiresItsOwnCredentialAndNeverExposesControl(t *testing.T) {
	state, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	principal, credential, err := state.Create("client", workspace)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := publications.OpenInline([]byte(`{"workspaces":["research"]}`), func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	ingress := NewPublicWorkspaceHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if _, err := WorkspaceServiceAuthorityFromContext(r.Context(), workspace, routing.ServiceModels); err != nil {
			t.Errorf("allowed Workspace request was not admitted: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}), state, publication, nil)
	send := func(path, bearer string) int {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "https://relay.test"+path, nil)
		request.Header.Set("Authorization", "Bearer "+bearer)
		// Forwarded headers must not create control-plane authority.
		request.Header.Set("X-Forwarded-Host", "127.0.0.1:7926")
		request.Header.Set("X-Forwarded-User", "local_operator")
		response := httptest.NewRecorder()
		ingress.ServeHTTP(response, request)
		return response.Code
	}
	for _, path := range []string{"/api/v1/workspaces", "/api/v1/telemetry/report", "/api/v1/daemon/shutdown", "/c/research/v1/models", "/api/v1/principals", "/api/v1/workspaces/research/publications", "/_swobu/down", "/app/", "/workspaces/research/api/v1/status"} {
		if code := send(path, credential.Token); code != http.StatusNotFound {
			t.Fatalf("published control path %q yielded %d", path, code)
		}
	}
	if calls != 0 {
		t.Fatal("public control request reached the data handler")
	}
	if code := send("/workspaces/research/v1/models", "invalid"); code != http.StatusUnauthorized {
		t.Fatalf("uncredentialed data path=%d", code)
	}
	if code := send("/workspaces/research/v1/models", credential.Token); code != http.StatusAccepted {
		t.Fatalf("valid data path=%d", code)
	}
	if calls != 1 {
		t.Fatalf("data-plane calls=%d", calls)
	}
	if _, err := state.RevokeCredential(principal.ID, credential.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if code := send("/workspaces/research/v1/models", credential.Token); code != http.StatusUnauthorized {
		t.Fatalf("revoked bearer data path=%d", code)
	}
	if calls != 1 {
		t.Fatal("revoked request reached backend")
	}
	current := publication.Revision()
	if err := publication.Set(workspace, false, current, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if code := send("/workspaces/research/v1/models", credential.Token); code != http.StatusNotFound {
		t.Fatalf("unpublished data path=%d", code)
	}
}

func TestPublishedWorkspaceHonorsIndependentServiceGrants(t *testing.T) {
	state, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := routing.ParseWorkspaceSlug("research")
	principal, credential, err := state.Create("model-only", ws)
	if err != nil {
		t.Fatal(err)
	}
	version, err := state.PermissionRevision(principal.ID, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, version, "models-only", func() error { return nil }, []routing.ServiceKind{routing.ServiceModels}, nil); err != nil {
		t.Fatal(err)
	}
	published, err := publications.OpenInline([]byte(`{"workspaces":["research"]}`), func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	attempts := 0
	ingress := NewPublicWorkspaceHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { attempts++; w.WriteHeader(http.StatusAccepted) }), state, published, nil)
	for _, probe := range []struct {
		method, path string
		admitted     bool
	}{
		{http.MethodGet, "/workspaces/research/v1/models", true},
		{http.MethodPost, "/workspaces/research/v1/responses", true},
		{http.MethodPost, "/workspaces/research/search", false},
		{http.MethodPost, "/workspaces/research/mcp", false},
	} {
		req := httptest.NewRequest(probe.method, "https://relay.test"+probe.path, nil)
		req.Header.Set("Authorization", "Bearer "+credential.Token)
		resp := httptest.NewRecorder()
		ingress.ServeHTTP(resp, req)
		if probe.admitted && resp.Code != http.StatusAccepted || !probe.admitted && resp.Code == http.StatusAccepted {
			t.Fatalf("%s %s status=%d allowed=%v", probe.method, probe.path, resp.Code, probe.admitted)
		}
	}
	if attempts != 2 {
		t.Fatalf("backend attempts=%d want 2", attempts)
	}
}

func TestPublishedWorkspaceRoutesModelMCPAndSearchThroughOneAdmissionBoundary(t *testing.T) {
	state, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := routing.ParseWorkspaceSlug("engineering")
	principal, bearer, err := state.Create("tool-client", workspace)
	if err != nil {
		t.Fatal(err)
	}
	version, err := state.PermissionRevision(principal.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, workspace, version, "mcp-search-only", func() error { return nil }, []routing.ServiceKind{routing.ServiceMCP, routing.ServiceSearch}, nil); err != nil {
		t.Fatal(err)
	}
	published, err := publications.OpenInline([]byte(`{"workspaces":["engineering","personal"]}`), func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	var dispatched []routing.ServiceKind
	incoming := NewPublicWorkspaceHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service := requestedWorkspaceService(r.URL.Path)
		if _, err := WorkspaceServiceAuthorityFromContext(r.Context(), workspace, service); err != nil {
			t.Errorf("admitted %s without scoped authority: %v", service, err)
		}
		dispatched = append(dispatched, service)
		w.WriteHeader(http.StatusAccepted)
	}), state, published, nil)
	for _, tc := range []struct {
		method, path string
		ok           bool
	}{
		{"POST", "/workspaces/engineering/mcp", true},
		{"POST", "/workspaces/engineering/search", true},
		{"GET", "/workspaces/engineering/v1/models", false},
		{"POST", "/workspaces/engineering/v1/responses", false},
		{"POST", "/workspaces/personal/search", false}, // published, wrong Workspace audience
		{"GET", "/workspaces/engineering/mcp", false},  // not a data operation
		{"POST", "/workspaces/engineering/search/extra", false},
		{"POST", "/workspaces/engineering/api/v1/principals", false},
		{"GET", "/workspaces/engineering/v1/../v1/models", false},
		{"GET", "/workspaces/engineering/v1/models/../models", false},
	} {
		req := httptest.NewRequest(tc.method, "https://relay.test"+tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+bearer.Token)
		response := httptest.NewRecorder()
		incoming.ServeHTTP(response, req)
		if (response.Code == http.StatusAccepted) != tc.ok {
			t.Errorf("%s %s status=%d want-admitted=%v", tc.method, tc.path, response.Code, tc.ok)
		}
	}
	if len(dispatched) != 2 || dispatched[0] != routing.ServiceMCP || dispatched[1] != routing.ServiceSearch {
		t.Fatalf("dispatched services = %v", dispatched)
	}
	current := published.Revision()
	if err := published.Set(workspace, false, current, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "https://relay.test/workspaces/engineering/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+bearer.Token)
	response := httptest.NewRecorder()
	incoming.ServeHTTP(response, req)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unpublished MCP status %d", response.Code)
	}
}

func TestPublicPreflightNeverEscapesPublicationOrOperation(t *testing.T) {
	published, err := publications.OpenInline([]byte(`{"workspaces":["engineering"]}`), func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	allowed := []struct {
		method, path, advertised string
		ok                       bool
	}{
		{"OPTIONS", "/workspaces/engineering/v1/responses", "POST", true},
		{"OPTIONS", "/workspaces/engineering/mcp", "POST", true},
		{"OPTIONS", "/workspaces/engineering/search", "POST", true},
		{"OPTIONS", "/workspaces/engineering/v1/models", "GET", true},
		{"OPTIONS", "/workspaces/engineering/mcp", "DELETE", false},
		{"OPTIONS", "/workspaces/personal/search", "POST", false},
		{"OPTIONS", "/api/v1/workspaces/engineering/publications", "GET", false},
		{"OPTIONS", "/workspaces/engineering/search/extra", "POST", false},
	}
	for _, tc := range allowed {
		req := httptest.NewRequest(tc.method, "https://relay.test"+tc.path, nil)
		req.Header.Set("Origin", "https://app.test")
		req.Header.Set("Access-Control-Request-Method", tc.advertised)
		if got := publicWorkspacePreflightAllowed(published, req); got != tc.ok {
			t.Errorf("%s %s preflight for %s = %v", tc.method, tc.path, tc.advertised, got)
		}
	}
}
