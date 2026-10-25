package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// Exercise the assembled public handler independently of the local listener.
// Neither a Workspace bearer nor browser-cookie shape can turn the public data
// listener into local control or an enabled remote administration listener.
func TestPublicListenerRejectsControlAndAdminAcrossCredentialShapes(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	seedTestWorkspaces(t, configPath, "research")
	daemon, err := Start(t.Context(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	workspace, _ := routing.ParseWorkspaceSlug("research")
	_, issued, err := daemon.accessStore.Create("consumer", workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/connections", "/api/v1/connections/example/references", "/api/v1/status", "/api/v1/telemetry/report", "/api/v1/daemon/shutdown", "/api/v1/workspaces", "/api/v1/browser-launches", "/api/v1/browser-session", "/_swobu/status", "/admin/mcp", "/open", "/workspaces/research/api/v1/status", "/workspaces/research/admin/mcp"} {
		for _, kind := range []string{"none", "workspace", "cookie", "node-shaped"} {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				t.Run(method+path+kind, func(t *testing.T) {
					r := httptest.NewRequest(method, "https://public.example"+path, strings.NewReader(`{}`))
					switch kind {
					case "workspace":
						r.Header.Set("Authorization", "Bearer "+issued.Token)
					case "cookie":
						r.AddCookie(&http.Cookie{Name: "swobu_operator_session", Value: strings.Repeat("x", 43)})
					case "node-shaped":
						r.Header.Set("Authorization", "Bearer node_unrecognized")
					}
					w := httptest.NewRecorder()
					daemon.publicHandler.ServeHTTP(w, r)
					// The retained legacy Share boundary may challenge unknown
					// paths before routing. N02 owns its deletion; both responses
					// deny access without exposing local or admin handlers.
					if w.Code != http.StatusNotFound && w.Code != http.StatusUnauthorized {
						t.Fatalf("public listener admitted %s %s credential=%s: %d", method, path, kind, w.Code)
					}
					if w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "<!doctype") || strings.Contains(w.Body.String(), "launch_path") {
						t.Fatal("public listener exposed browser control material")
					}
				})
			}
		}
	}
}

// AI-driven admin sessions were never part of remote Node administration.
// Their retirement must not leave a reachable local inference or limit mutation
// endpoint; direct Workspace Limits preview/apply remains independently owned.
func TestRetiredAgentSessionEndpointsAreNotRegistered(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	seedTestWorkspaces(t, configPath, "research")
	daemon, err := Start(t.Context(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/agent-sessions"},
		{http.MethodGet, "/api/v1/agent-sessions/unused"},
		{http.MethodPost, "/api/v1/agent-sessions/unused/infer"},
		{http.MethodGet, "/api/v1/agent-sessions/unused/limits/suggestion"},
		{http.MethodPost, "/api/v1/agent-sessions/unused/limits/preview"},
		{http.MethodPost, "/api/v1/agent-sessions/unused/limits/apply"},
	} {
		t.Run(endpoint.method+" "+endpoint.path, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), endpoint.method, daemon.BaseURL()+endpoint.path, strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("retired agent-session endpoint returned %d, want 404", res.StatusCode)
			}
		})
	}
}

// Unmounted design-era Control paths must remain absent on the real local
// listener, even where a broad Workspace or Principal mux prefix still exists.
// A future API proposal cannot create an authorization alias by accident.
func TestUnimplementedControlTargetsReturnNotFound(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	seedTestWorkspaces(t, path, "research")
	daemon, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/workspaces/research/grants"},
		{http.MethodPut, "/api/v1/workspaces/research/grants/no-principal"},
		{http.MethodGet, "/api/v1/principals/no-principal/grants"},
		{http.MethodPost, "/api/v1/node-admin-credentials"},
		{http.MethodGet, "/api/v1/node-admin-grants"},
		{http.MethodPost, "/api/v1/workspaces/research/credentials"},
		{http.MethodPost, "/api/v1/workspaces/research/enrollments"},
		{http.MethodPost, "/api/v1/client-setup-applies"},
		{http.MethodPut, "/api/v1/settings/otel"},
	} {
		t.Run(endpoint.method+" "+endpoint.path, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), endpoint.method, daemon.BaseURL()+endpoint.path, strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("unmounted %s %s gave %d, want 404", endpoint.method, endpoint.path, response.StatusCode)
			}
		})
	}
}
