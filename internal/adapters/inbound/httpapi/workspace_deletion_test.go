package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// Remove only the exact empty obstruction created above, then restore the
// original routing bytes. No authority store is rolled back.

func TestVersionedWorkspaceDeletionPreviewPreconditionsAndRestartReplay(t *testing.T) {
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
	if _, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "research"}, "fixture-operator", "fixture-4689", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	mux := NewNodeControlAPIHandler(service)
	authorized := true
	checks := 0
	authority := admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
		checks++
		if !authorized {
			return accessidentity.ID{}, errors.New("revoked")
		}
		return accessidentity.Local, nil
	})
	request := func(query, etag, key string, admitted bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/research"+query, strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		if etag != "" {
			r.Header.Set("If-Match", etag)
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		if admitted {
			r = r.WithContext(contextWithAuthority(r, authority))
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	preview := request("?dry_run=true", "", "", true)
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), `"approval_class":"high_risk"`) {
		t.Fatalf("preview %d: %s", preview.Code, preview.Body.String())
	}
	etag := preview.Header().Get("ETag")
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preview mutated routing")
	}
	for _, tc := range []struct {
		query, etag, key string
		admitted         bool
		status           int
	}{
		{"", etag, "key", false, 403},
		{"", "", "key", true, 428},
		{"", etag, "", true, 400},
		{"", "*", "key", true, 400},
		{"", `W/` + etag, "key", true, 400},
		{"", `"stale"`, "key", true, 412},
		{"?dry_run=maybe", etag, "key", true, 400},
		{"?dry_run=true&dry_run=false", etag, "key", true, 400},
		{"?dry_run=%zz", etag, "key", true, 400},
	} {
		w := request(tc.query, tc.etag, tc.key, tc.admitted)
		if w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
	if _, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "unrelated"}, "fixture-operator", "fixture-6681", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if w := request("?dry_run=true", "", "", true); w.Header().Get("ETag") != etag {
		t.Fatal("unrelated edit invalidated preview")
	}
	// Real assembled consumer obtains server impact and applies exactly that
	// ETag. This test does not substitute for interactive human confirmation.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(contextWithAuthority(r, authority)))
	}))
	defer server.Close()
	client := operatorclient.New(server.Client(), server.URL)
	p, err := client.PreviewWorkspaceDeletion(t.Context(), "research")
	if err != nil || `"`+p.ManagementETag+`"` != etag {
		t.Fatalf("consumer preview: %+v %v", p, err)
	}
	checks = 0
	if err := client.ApplyWorkspaceDeletion(t.Context(), "research", p.ManagementETag, "key"); err != nil {
		t.Fatal(err)
	}
	if checks != 2 {
		t.Fatalf("admission checks=%d; require ingress and commit", checks)
	}
	server.Close()
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
	if _, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "research"}, "fixture-operator", "fixture-7978", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	mux = NewNodeControlAPIHandler(service)
	if w := request("", etag, "key", true); w.Code != 200 {
		t.Fatalf("restart replay %d %s", w.Code, w.Body.String())
	}
	if _, ok := store.Config().Workspace(mustWorkspaceSlug(t, "research")); !ok {
		t.Fatal("replay deleted recreated lifetime")
	}
	if w := request("", etag, "another-key", true); w.Code != 412 {
		t.Fatalf("old lifetime: %d", w.Code)
	}
	if w := request("", `"changed"`, "key", true); w.Code != 409 {
		t.Fatalf("changed intent: %d", w.Code)
	}
	// Revoke between ingress and transaction admission, not just before dispatch.
	checks = 0
	authority = func(routing.WorkspaceSlug) (accessidentity.ID, error) {
		checks++
		if checks > 1 {
			return accessidentity.ID{}, errors.New("revoked")
		}
		return accessidentity.Local, nil
	}
	if w := request("", etag, "key", true); w.Code != 403 || checks != 2 {
		t.Fatalf("effect-time admission: %d checks=%d", w.Code, checks)
	}
}
