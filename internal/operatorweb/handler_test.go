package operatorweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesClosedSPARoutesWithDocumentPolicy(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/", "/workspaces", "/workspaces/dev/overview", "/workspaces/dev/services", "/workspaces/dev/services/models", "/workspaces/dev/services/models/coding", "/workspaces/dev/services/models/deepseek%2F*", "/workspaces/dev/access", "/workspaces/dev/limits", "/workspaces/dev/connect", "/connections", "/connections/cn_test", "/access", "/access/alice", "/activity", "/activity/ex_retained", "/settings/history", "/settings/network", "/settings/administration"} {
		t.Run(route, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `<div id="root"></div>`) {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
			for _, name := range []string{"Content-Security-Policy", "Cross-Origin-Resource-Policy", "Referrer-Policy", "X-Content-Type-Options", "X-Frame-Options"} {
				if response.Header().Get(name) == "" {
					t.Fatalf("missing %s", name)
				}
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("cache = %q", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestHandlerNeverTurnsUnknownControlOrInferencePathsIntoSPA(t *testing.T) {
	handler, _ := NewHandler()
	for _, route := range []string{"/_swobu/unknown", "/workspaces/unknown", "/totally-unknown", "/workspaces/a/unknown", "/workspaces/dev", "/workspaces/dev/routes/coding", "/shares", "/connect", "/runtime", "/workspaces/dev/v1", "/workspaces/dev/mcp", "/workspaces/dev/search", "/api/v1/node", "/workspaces/dev/services/unknown"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(method, route, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s %s status=%d", method, route, response.Code)
			}
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / status = %d", response.Code)
	}
}

func TestHandlerServesHashedAssetsImmutably(t *testing.T) {
	handler, _ := NewHandler()
	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	start := strings.Index(index.Body.String(), "/assets/")
	if start < 0 {
		t.Fatal("index has no asset")
	}
	end := strings.IndexAny(index.Body.String()[start:], `"'`)
	asset := index.Body.String()[start : start+end]
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, asset, nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset = %d %q", response.Code, response.Header().Get("Cache-Control"))
	}
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing = %d", missing.Code)
	}
}

func TestSPARouteNeverStealsWorkspaceDataPlane(t *testing.T) {
	for _, route := range []string{"/workspaces/engineering/v1", "/workspaces/engineering/v1/chat/completions", "/workspaces/engineering/mcp", "/workspaces/engineering/search", "/workspaces/engineering/responses", "/workspaces/engineering/services/models/route/extra"} {
		if IsSPARoute(route) {
			t.Errorf("client endpoint incorrectly classified as UI: %s", route)
		}
	}
}

func TestOperatorLogoIsNotMistakenForSPAOrImmutableAssets(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/swobu.png", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("logo status=%d headers=%v", response.Code, response.Header())
	}
	if response.Body.Len() < 8 || !strings.HasPrefix(response.Body.String(), "\x89PNG\r\n\x1a\n") {
		t.Fatal("invalid logo")
	}
}
