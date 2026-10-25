package operatorclient

import (
	"context"
	"encoding/json"
	"fmt"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetSearchRouteUsesCanonicalRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Path != "/api/v1/workspaces/research" {
				t.Fatalf("read path %s", r.URL.Path)
			}
			fmt.Fprint(w, `{"slug":"research","management_etag":"e-1"}`)
		case http.MethodPut:
			if r.URL.Path != "/api/v1/workspaces/research/search-route" || r.Header.Get("If-Match") != `"e-1"` {
				t.Fatalf("unexpected checked request %s %s etag=%q", r.Method, r.URL.RequestURI(), r.Header.Get("If-Match"))
			}
			var body struct {
				ConnectionID string   `json:"connection_id"`
				Fallback     []string `json:"fallback_connection_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ConnectionID != "cn_search" || len(body.Fallback) != 2 {
				t.Fatalf("Search route payload=%+v error=%v", body, err)
			}
			fmt.Fprint(w, `{"slug":"research","management_etag":"e-2"}`)
		default:
			t.Fatalf("method %s", r.Method)
		}
	}))
	defer server.Close()
	client := New(server.Client(), server.URL)
	if _, err := client.SetSearchRoute(context.Background(), "research", "cn_search", []string{"cn_secondary", "cn_tertiary"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetSearchRoute(context.Background(), "research", "search", nil); err == nil || !strings.Contains(err.Error(), "Connection") {
		t.Fatal("invalid Search Connection reached transport")
	}
}

func TestMCPMountClientUsesOneAliasResourceAndWorkspaceReview(t *testing.T) {
	const want = "/api/v1/workspaces/research/mcp-mounts/github"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPut || r.URL.Path != want {
			t.Errorf("wrong MCP mount resource: %s %s", r.Method, r.URL.RequestURI())
			http.Error(w, "incorrect mount path", http.StatusNotFound)
			return
		}
		if r.Header.Get("Idempotency-Key") != "" {
			t.Error("removed replay identity was transmitted")
		}
		switch r.URL.Query().Get("dry_run") {
		case "true":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"revision":"life-5","digest":"review-digest","connection_revision":"7"}`)
		case "false":
			if r.Header.Get("If-Match") != `"life-5"` {
				t.Errorf("missing reviewed Workspace ETag: %q", r.Header.Get("If-Match"))
			}
			var body struct {
				Alias        string `json:"alias"`
				ConnectionID string `json:"connection_id"`
				Review       struct {
					WorkspaceETag      string `json:"workspace_etag"`
					Digest             string `json:"digest"`
					ConnectionRevision string `json:"connection_revision"`
				} `json:"review"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode mount body: %v", err)
			}
			if body.Alias != "github" || body.ConnectionID != "cn_mcp" || body.Review.WorkspaceETag != "life-5" || body.Review.Digest != "review-digest" || body.Review.ConnectionRevision != "7" {
				t.Errorf("mount apply changed reviewed identity: %+v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"slug":"research","management_etag":"life-6"}`)
		default:
			t.Errorf("missing explicit dry_run query: %s", r.URL.RawQuery)
		}
	}))
	defer server.Close()
	client := New(server.Client(), server.URL)
	preview, err := client.PreviewMCPConnectionMount(context.Background(), "research", "github", "cn_mcp", []string{"search_code"})
	if err != nil || preview.Revision != "life-5" {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	apply, err := client.ApplyMCPConnectionMount(context.Background(), "research", "github", "cn_mcp", []string{"search_code"}, workspaceapi.MCPMountReviewRequest{WorkspaceETag: preview.Revision, Digest: preview.Digest, ConnectionRevision: preview.ConnectionRevision})
	if err != nil || apply.Slug != "research" {
		t.Fatalf("apply: %+v, %v", apply, err)
	}
	if requests != 2 {
		t.Fatalf("expected preview and apply, got %d", requests)
	}
}
