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
)

func TestModelRoutePUTUsesWorkspaceETagWithoutReplayIdentity(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "routing.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := workspaces.NewService(store, workspaces.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	target := workspaces.TargetDraft{ID: "original", Model: "fixture", Protocol: "responses", ConnectionID: "cn_fixture"}
	reviewed, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "research", InitialRoute: "chat", Target: target}, "fixture", "create-key", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	spec, err := service.GetRoute(t.Context(), "research", "chat")
	if err != nil {
		t.Fatal(err)
	}
	spec.Tiers[0].Targets[0].Model = "replacement-model"
	raw, _ := json.Marshal(spec)
	mux := NewNodeControlAPIHandler(service)
	before, _ := os.ReadFile(path)
	for _, mode := range []string{"denied", "missing-revision", "weak", "stale", "query"} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/research/model-routes/chat", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+reviewed.ManagementETag+`"`)
		status := 400
		if mode != "denied" {
			req = permissionOperatorRequest(req)
		}
		switch mode {
		case "denied":
			status = 403
		case "missing-revision":
			req.Header.Del("If-Match")
			status = 428
		case "weak":
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", `W/"weak"`)
			status = 428
		case "stale":
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", `"stale"`)
			status = 412
		case "query":
			req.URL.RawQuery = "dry_run=false"
		}
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		after, _ := os.ReadFile(path)
		if rr.Code != status || !bytes.Equal(before, after) {
			t.Fatalf("%s: status=%d body=%s", mode, rr.Code, rr.Body.String())
		}
	}
	req := permissionOperatorRequest(httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/research/model-routes/chat", bytes.NewReader(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+reviewed.ManagementETag+`"`)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
	}
	var committed workspaces.Workspace
	if err := json.Unmarshal(rr.Body.Bytes(), &committed); err != nil {
		t.Fatal(err)
	}
	if committed.ManagementRevision != reviewed.ManagementRevision+1 || rr.Header().Get("ETag") != `"`+committed.ManagementETag+`"` {
		t.Fatal("revision missing")
	}
	var payload map[string]any
	json.Unmarshal(rr.Body.Bytes(), &payload)
	if _, exists := payload["replayed"]; exists {
		t.Fatal("duplicate replay state")
	}
	req = permissionOperatorRequest(httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/research/model-routes/chat", bytes.NewReader(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+reviewed.ManagementETag+`"`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 412 {
		t.Fatalf("stale revision succeeded: %d", rr.Code)
	}
}
