package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/publications"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestPublicationIsWorkspaceReachabilityNotASeparateGrant(t *testing.T) {
	var persisted []byte
	store, err := publications.OpenInline(nil, func(raw []byte) error { persisted = bytes.Clone(raw); return nil })
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := routing.ParseWorkspaceSlug("engineering")
	checks := 0
	released := 0
	stopped := 0
	h := PublicationHandler{Store: store, Exists: func(candidate routing.WorkspaceSlug) bool { return candidate == ws },
		Ready:          func(context.Context) (func(), error) { checks++; return func() { released++ }, nil },
		StopIfInactive: func() { stopped++ }, RelayURL: func() string { return "https://relay.test" }}
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/workspaces/{workspace}/publications", h)
	mux.Handle("PUT /api/v1/workspaces/{workspace}/publications/relay", h)
	mux.Handle("DELETE /api/v1/workspaces/{workspace}/publications/relay", h)
	base := "/api/v1/workspaces/engineering/publications"
	call := func(method, path, revision string, operator bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if revision != "" {
			req.Header.Set("If-Match", revision)
		}
		if operator {
			req = req.WithContext(contextWithAuthority(req, func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil }))
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		return response
	}
	if got := call(http.MethodGet, base, "", false); got.Code != http.StatusForbidden {
		t.Fatalf("unauthorized GET: %d", got.Code)
	}
	before := call(http.MethodGet, base, "", true)
	if before.Code != http.StatusOK {
		t.Fatalf("GET before: %d %s", before.Code, before.Body.String())
	}
	var first struct {
		Published bool   `json:"published"`
		Workspace string `json:"workspace"`
		Revision  string `json:"revision"`
	}
	if err := json.Unmarshal(before.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Published || first.Workspace != "engineering" || first.Revision == "" {
		t.Fatalf("unexpected state: %+v", first)
	}
	if got := call(http.MethodPut, base+"/relay", "", true); got.Code != http.StatusPreconditionRequired {
		t.Fatalf("no CAS: %d", got.Code)
	}
	if got := call(http.MethodPut, base+"/relay", `W/"`+first.Revision+`"`, true); got.Code != http.StatusPreconditionRequired {
		t.Fatalf("weak CAS: %d", got.Code)
	}
	if got := call(http.MethodPut, base+"/relay", `"`+first.Revision+`"`, true); got.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", got.Code, got.Body.String())
	}
	if checks != 1 || released != 1 || len(persisted) == 0 || !store.Has(ws) {
		t.Fatalf("publication readiness/commit mismatch %d %d %s", checks, released, persisted)
	}
	if bytes.Contains(persisted, []byte("anonymous")) || bytes.Contains(persisted, []byte("protocols")) {
		t.Fatal("publication contains shadow authorization")
	}
	if got := call(http.MethodDelete, base+"/relay", `"`+first.Revision+`"`, true); got.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale CAS: %d", got.Code)
	}
	latest := call(http.MethodGet, base, "", true)
	var current struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(latest.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if got := call(http.MethodDelete, base+"/relay", `"`+current.Revision+`"`, true); got.Code != http.StatusOK {
		t.Fatalf("unpublish: %d %s", got.Code, got.Body.String())
	}
	if store.Has(ws) || stopped != 1 {
		t.Fatalf("publication remained active: %v stop=%d", store.Has(ws), stopped)
	}
	if got := call(http.MethodGet, "/api/v1/workspaces/unknown/publications", "", true); got.Code != http.StatusNotFound {
		t.Fatalf("unknown Workspace: %d", got.Code)
	}
}

func TestPublicationCannotAppearIfRelayReadinessFails(t *testing.T) {
	store, _ := publications.OpenInline(nil, func([]byte) error { return errors.New("unexpected write") })
	h := PublicationHandler{Store: store, Exists: func(routing.WorkspaceSlug) bool { return true }, Ready: func(context.Context) (func(), error) { return nil, errors.New("relay offline") }}
	ws, _ := routing.ParseWorkspaceSlug("engineering")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/engineering/publications/relay", nil)
	req.SetPathValue("workspace", "engineering")
	req.Header.Set("If-Match", `"`+store.Revision()+`"`)
	req = req.WithContext(contextWithAuthority(req, func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || store.Has(ws) {
		t.Fatalf("relay failure committed a publication: status=%d", rec.Code)
	}
}
