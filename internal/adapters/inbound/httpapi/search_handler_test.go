package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/search"
)

func TestWorkspaceSearchHandlerUsesTypedSourceAndFailsClosed(t *testing.T) {
	service := search.Service{
		Sources:       map[string]search.Source{"research": search.StaticSource{{URL: "https://go.dev", Title: "Go", Snippet: "language"}}},
		ConnectionIDs: func(workspace string) ([]string, bool) { return []string{"research"}, workspace == "research" },
	}
	handler := NewWorkspaceSearchHandler(service, func(_ *http.Request, workspace string) bool { return workspace == "research" })
	req := httptest.NewRequest(http.MethodPost, "/workspaces/research/search", strings.NewReader(`{"query":"go","limit":1}`))
	req.SetPathValue("workspace", "research")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "go.dev") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	missing := httptest.NewRequest(http.MethodPost, "/workspaces/coding/search", strings.NewReader(`{"query":"go"}`))
	missing.SetPathValue("workspace", "coding")
	missingRes := httptest.NewRecorder()
	handler.ServeHTTP(missingRes, missing)
	if missingRes.Code != http.StatusNotFound {
		t.Fatalf("status=%d", missingRes.Code)
	}
}

func TestWorkspaceSearchGETUsesTheSameServiceAndRejectsAmbiguousQueries(t *testing.T) {
	service := search.Service{Sources: map[string]search.Source{"docs": search.StaticSource{{Title: "Go", URL: "https://go.dev"}}}, ConnectionIDs: func(string) ([]string, bool) { return []string{"docs"}, true }}
	handler := NewWorkspaceSearchHandler(service, func(*http.Request, string) bool { return true })
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"query=go&limit=1", 200}, {"query=go&query=other", 400}, {"query=go&limit=invalid", 400}, {"query=go&url=https://other.test", 400}, {"query=go&limit=1&limit=2", 400}, {"query=%zz", 400},
	} {
		t.Run(tc.query, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/workspaces/research/search?"+tc.query, nil)
			req.SetPathValue("workspace", "research")
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status=%d body=%s", res.Code, res.Body)
			}
		})
	}
}
