package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
)

func TestPermissionChangedPayloadAfterRestartConflictsWithoutPublication(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	routingStore, err := configstore.OpenOrCreate(filepath.Join(dir, "swobu.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = routingStore.Close() })
	workspaceService, err := workspaces.NewService(routingStore, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := workspaceService.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "research"}, "fixture-operator", "fixture-893", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "access.json")
	state, err := openTestAccessState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	service, err := accessapp.NewService(state, routingStore, &routebindings.Coordinator{})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := service.Create(t.Context(), "restart-contract", "research")
	if err != nil {
		t.Fatal(err)
	}
	permission, err := service.ReadPermission(principal.Access.ID, "research")
	if err != nil {
		t.Fatal(err)
	}
	originalRevision := permission.Revision
	start := func(service accessapp.Service) *httptest.Server {
		server := httptest.NewServer(NewLocalAdmissionHandler(NewAccessControlHandler(service), nil, nil))
		t.Cleanup(server.Close)
		return server
	}
	request := func(server *httptest.Server, payload string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, server.URL+"/api/v1/workspaces/research/permissions/"+principal.Access.ID, strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+originalRevision+`"`)
		req.Header.Set("Idempotency-Key", "restart-payload-key")
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("status=%d want=%d", response.StatusCode, want)
		}
		if want == http.StatusOK {
			var result accessapp.PermissionMutation
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatal(err)
			}
			if result.Principal != principal.Access.ID || result.Workspace != "research" || result.Permission == nil || result.Revision == "" || response.Header.Get("ETag") != `"`+result.Revision+`"` || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("mutation response omitted authoritative relationship/ETag")
			}
			if bytes.Contains(body, []byte("swak_")) || bytes.Contains(body, []byte("digest")) {
				t.Fatal("mutation response exposed credential material")
			}
		}
	}
	original := `{"action":"use","expires_at":"2030-01-01T00:00:00Z"}`
	first := start(service)
	request(first, original, http.StatusOK)
	first.Close()
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct authority and HTTP admission from disk, not the retained
	// service, so replay cannot pass merely because of in-memory receipts.
	reopened, err := openTestAccessState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := accessapp.NewService(reopened, routingStore, &routebindings.Coordinator{})
	if err != nil {
		t.Fatal(err)
	}
	second := start(resumed)
	request(second, original, http.StatusOK)
	request(second, `{"action":"use","expires_at":"2031-01-01T00:00:00Z"}`, http.StatusConflict)
	request(second, `{"action":"use"}`, http.StatusConflict)
	request(second, original, http.StatusOK)
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("replay or conflicting payload published authority or receipt metadata")
	}
	if resumed.Revision() != service.Revision() {
		t.Fatal("conflicting payload changed authority revision")
	}
}
