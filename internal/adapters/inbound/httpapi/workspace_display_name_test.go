package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestWorkspaceCreationDisplayMetadataSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
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
	create := func(body string) *httptest.ResponseRecorder {
		req := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "creation-metadata")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	for _, body := range []string{`{"slug":"invalid","display_name":"bad\nname"}`, `{"slug":"invalid","display_name":false}`} {
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		response := create(body)
		after, err := os.ReadFile(path)
		if err != nil || response.Code != 400 || string(before) != string(after) {
			t.Fatal("invalid creation published state", response.Code)
		}
	}
	response := create(`{"slug":"research","display_name":"Research 東京"}`)
	var created workspaces.Workspace
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &created) != nil || created.DisplayName != "Research 東京" || created.Slug != "research" || len(created.Routes) != 0 || created.ManagementETag == "" {
		t.Fatal("creation metadata/empty capability projection invalid")
	}
	if response.Header().Get("Location") != "/api/v1/workspaces/research" || response.Header().Get("ETag") != `"`+created.ManagementETag+`"` || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("creation omitted canonical resource/concurrency headers")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = configstore.OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err = workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := service.GetWorkspace(t.Context(), "research")
	if err != nil || reopened.DisplayName != created.DisplayName || reopened.ManagementETag != created.ManagementETag {
		t.Fatal("creation metadata lost on reopen")
	}
}

func TestWorkspaceCreationRejectsUnsupportedQueryWithoutPublication(t *testing.T) {
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
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range []struct {
		prefix  string
		handler http.Handler
	}{
		{"/api/v1", NewNodeControlAPIHandler(service)}, {"/_swobu", NewNodeControlAPIHandler(service)},
	} {
		for _, query := range []string{"dry_run=true", "dry_run=false", "dry_run=invalid", "dry_run=true&dry_run=false", "unknown=1", "%zz"} {
			if surface.prefix == "/api/v1" && query == "dry_run=true" {
				continue
			}
			req := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, surface.prefix+"/workspaces", strings.NewReader(`{"slug":"research"}`)))
			req.URL.RawQuery = query
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			surface.handler.ServeHTTP(response, req)
			after, err := os.ReadFile(path)
			want := http.StatusBadRequest
			if surface.prefix == "/_swobu" {
				want = http.StatusNotFound
			}
			if err != nil || response.Code != want || string(before) != string(after) || len(store.Config().Workspaces()) != 0 {
				t.Fatalf("unsupported creation query published state: surface=%s query=%s status=%d", surface.prefix, query, response.Code)
			}
		}
	}
}

func TestWorkspaceSlugRenameIsNotMountedOnEitherControlSurface(t *testing.T) {
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
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "research", DisplayName: "Research"}, "fixture-operator", "fixture-4691", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range []struct {
		prefix  string
		handler http.Handler
	}{{"/api/v1", NewNodeControlAPIHandler(service)}, {"/_swobu", NewNodeControlAPIHandler(service)}} {
		req := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, surface.prefix+"/workspaces/research/rename", strings.NewReader(`{"new_slug":"renamed"}`)))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		surface.handler.ServeHTTP(response, req)
		if response.Code != 404 {
			t.Fatalf("retired slug rename status=%d on %s", response.Code, surface.prefix)
		}
		after, err := os.ReadFile(path)
		current, readErr := service.GetWorkspace(t.Context(), "research")
		if err != nil || readErr != nil || string(before) != string(after) || current.ManagementETag != created.ManagementETag || current.DisplayName != created.DisplayName {
			t.Fatal("retired rename changed durable identity")
		}
		if _, err := service.GetWorkspace(t.Context(), "renamed"); err == nil {
			t.Fatal("retired rename created another address")
		}
	}
}

func TestWorkspaceDisplayNamePATCHRequiresConsentAndRejectsSlugMutation(t *testing.T) {
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
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "research"}, "fixture-operator", "fixture-6373", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeControlAPIHandler(service)
	revision := `"` + created.ManagementETag + `"`
	before, _ := os.ReadFile(path)
	send := func(body, etag, query string, trusted bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/research"+query, strings.NewReader(body))
		if trusted {
			req = permissionOperatorRequest(req)
		}
		req.Header.Set("Content-Type", "application/json")
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	for _, probe := range []struct {
		body, etag, query string
		trusted           bool
		status            int
	}{
		{`{"display_name":"Research"}`, revision, "", false, 403},
		{`{"display_name":"Research"}`, "", "", true, 428},
		{`{"display_name":"Research"}`, `W/"weak"`, "", true, 400},
		{`{"display_name":"Research"}`, `"stale"`, "", true, 412},
		{`{"display_name":"Research"}`, revision, "?dry_run=true", true, 400},
		{`{"display_name":"Research","slug":"renamed"}`, revision, "", true, 400},
		{`{"display_name":"Research","new_slug":null}`, revision, "", true, 400},
		{`{}`, revision, "", true, 400}, {`{"display_name":null}`, revision, "", true, 400},
		{`{"display_name":false}`, revision, "", true, 400},
		{`{"display_name":"bad\nname"}`, revision, "", true, 400},
		{`{"display_name":"Research"}{}`, revision, "", true, 400},
	} {
		response := send(probe.body, probe.etag, probe.query, probe.trusted)
		if response.Code != probe.status {
			t.Fatalf("status=%d want=%d body=%s", response.Code, probe.status, response.Body.String())
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("rejected PATCH published bytes")
		}
	}
	checks := 0
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/research", strings.NewReader(`{"display_name":"Withdrawn"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", revision)
	req = req.WithContext(contextWithAuthority(req, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
		checks++
		if checks >= 2 {
			return accessidentity.ID{}, errors.New("withdrawn before publication")
		}
		return accessidentity.Local, nil
	})))
	denial := httptest.NewRecorder()
	handler.ServeHTTP(denial, req)
	deniedBytes, _ := os.ReadFile(path)
	if denial.Code != 403 || checks != 2 || string(before) != string(deniedBytes) {
		t.Fatal("transaction admission withdrawal did not prevent publication")
	}
	response := send(`{"display_name":"Research 東京","future_member":true}`, revision, "", true)
	var updated workspaces.Workspace
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &updated) != nil || updated.DisplayName != "Research 東京" || updated.Slug != "research" || response.Header().Get("ETag") != `"`+updated.ManagementETag+`"` || response.Header().Get("Cache-Control") != "no-store" || updated.ManagementETag == created.ManagementETag {
		t.Fatal("PATCH lost committed identity/metadata", response.Code, response.Body.String())
	}
	if send(`{"display_name":"Other"}`, revision, "", true).Code != 412 {
		t.Fatal("old consent refreshed")
	}
	before, _ = os.ReadFile(path)
	if send(`{"display_name":"Research 東京"}`, response.Header().Get("ETag"), "", true).Code != 200 {
		t.Fatal("valid no-op rejected")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("no-op wrote bytes")
	}
}
