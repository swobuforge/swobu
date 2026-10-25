package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestRequestedServiceClassificationAndCredentialAdmission(t *testing.T) {
	for path, kind := range map[string]routing.ServiceKind{
		"/workspaces/engineering/mcp":                                  routing.ServiceMCP,
		"/workspaces/engineering/search":                               routing.ServiceSearch,
		"/workspaces/engineering/v1/models":                            routing.ServiceModels,
		"/workspaces/engineering/v1/responses":                         routing.ServiceModels,
		"/workspaces/engineering/v1beta/models/gemini:generateContent": routing.ServiceModels,
		"/workspaces/engineering/chat/completions":                     routing.ServiceModels,
	} {
		if got := requestedWorkspaceService(path); got != kind {
			t.Fatalf("%s: %s, expected %s", path, got, kind)
		}
	}
	if got := requestedWorkspaceService("/workspaces/engineering/access"); got != "" {
		t.Fatalf("control-looking path classified as %s", got)
	}
	store, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := routing.ParseWorkspaceSlug("engineering")
	principal, credential, err := store.Create("models-only", ws)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(principal.ID, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "grant-models", true, func() error { return nil }, []routing.ServiceKind{routing.ServiceModels}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := NewLocalAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := WorkspaceAuthorityFromContext(r.Context(), ws); err != nil {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		// A Models request may project MCP tools only with a second, explicit
		// MCP grant, and that grant is checked again before tool execution.
		if _, err := WorkspaceServiceAuthorityFromContext(r.Context(), ws, routing.ServiceMCP); err == nil {
			http.Error(w, "MCP access escalated through Models", http.StatusForbidden)
			return
		}
		calls++
		w.WriteHeader(http.StatusNoContent)
	}), store, nil)
	for _, path := range []string{"/workspaces/engineering/mcp", "/workspaces/engineering/search"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer "+credential.Token)
		req.RemoteAddr = "127.0.0.1:49265"
		req.Host = "127.0.0.1:7926"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code < 400 || calls != 0 {
			t.Fatalf("%s accepted: status=%d calls=%d", path, rec.Code, calls)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/workspaces/engineering/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer "+credential.Token)
	req.RemoteAddr = "127.0.0.1:49265"
	req.Host = "127.0.0.1:7926"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || calls != 1 {
		t.Fatalf("Models denied: %d calls=%d", rec.Code, calls)
	}
}

func TestCrossServiceAuthorityIsRecheckedAtEffectTime(t *testing.T) {
	store, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := routing.ParseWorkspaceSlug("engineering")
	principal, issued, err := store.Create("models-and-tools", ws)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(principal.ID, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "initial-scope", true, func() error { return nil }, []routing.ServiceKind{routing.ServiceModels, routing.ServiceMCP}); err != nil {
		t.Fatal(err)
	}
	var contextAtAdmission context.Context
	handler := NewLocalAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextAtAdmission = r.Context()
		if _, err := WorkspaceServiceAuthorityFromContext(r.Context(), ws, routing.ServiceMCP); err != nil {
			http.Error(w, "MCP denied", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}), store, nil)
	req := httptest.NewRequest(http.MethodPost, "/workspaces/engineering/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer "+issued.Token)
	req.RemoteAddr = "127.0.0.1:49265"
	req.Host = "127.0.0.1:7926"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || contextAtAdmission == nil {
		t.Fatalf("initial tool authorization failed: status=%d", rec.Code)
	}
	revision, err = store.PermissionRevision(principal.ID, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, revision, "reduced-scope", true, func() error { return nil }, []routing.ServiceKind{routing.ServiceModels}); err != nil {
		t.Fatal(err)
	}
	if _, err := WorkspaceServiceAuthorityFromContext(contextAtAdmission, ws, routing.ServiceMCP); err == nil {
		t.Fatal("MCP was still authorized by a credential after its grant was narrowed")
	}
	if _, err := WorkspaceServiceAuthorityFromContext(contextAtAdmission, ws, routing.ServiceModels); err != nil {
		t.Fatalf("unrelated Models permission was lost: %v", err)
	}
}
