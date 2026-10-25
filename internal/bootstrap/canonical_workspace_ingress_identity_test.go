package bootstrap

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"strings"
	"sync/atomic"
	"testing"

	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCanonicalWorkspaceIngressSelectsExactPathWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}

	var alphaCalls, betaCalls atomic.Int32
	provider := func(name string, calls *atomic.Int32) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
				http.Error(w, "unexpected provider request", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":%q,"model":"fixture","output_text":%q}`, name+"-response", name))
		}))
	}
	alphaProvider := provider("alpha", &alphaCalls)
	defer alphaProvider.Close()
	betaProvider := provider("beta", &betaCalls)
	defer betaProvider.Close()

	configPath := filepath.Join(root, "swobu.yaml")
	writeCurrentMultiWorkspaceFixture(t, configPath, root, "responses", currentFixtureWorkspace{slug: "alpha", url: alphaProvider.URL}, currentFixtureWorkspace{slug: "beta", url: betaProvider.URL})
	daemon, err := Start(t.Context(), StartInput{
		ConfigPath:    configPath,
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	issue := func(slug, label string) string {
		t.Helper()
		workspace, err := routing.ParseWorkspaceSlug(slug)
		if err != nil {
			t.Fatal(err)
		}
		_, credential, err := daemon.accessStore.Create(label, workspace)
		if err != nil {
			t.Fatal(err)
		}
		return credential.Token
	}
	alphaToken := issue("alpha", "alpha-client")
	betaToken := issue("beta", "beta-client")

	request := func(workspace, token, wantText string, wantStatus int) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
			daemon.BaseURL()+"/workspaces/"+workspace+"/v1/responses",
			strings.NewReader(`{"model":"chat","input":"identity check","stream":false}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != wantStatus {
			t.Fatalf("Workspace %s status=%d want=%d body=%s", workspace, response.StatusCode, wantStatus, body)
		}
		if wantText != "" && !strings.Contains(string(body), wantText) {
			t.Fatalf("Workspace %s response=%s want selected-provider marker %q", workspace, body, wantText)
		}
	}

	request("alpha", alphaToken, "alpha", http.StatusOK)
	request("beta", betaToken, "beta", http.StatusOK)
	request("beta", alphaToken, "", http.StatusForbidden)
	for _, probe := range []struct {
		workspace  string
		credential string
		forwarded  map[string]string
	}{
		{
			workspace:  "beta",
			credential: alphaToken,
			forwarded: map[string]string{
				"X-Forwarded-Host": "alpha.example", "X-Forwarded-User": "beta-client", "X-Forwarded-Proto": "https",
			},
		},
		{
			workspace:  "alpha",
			credential: betaToken,
			forwarded: map[string]string{
				"X-Forwarded-Host": "beta.example", "X-Forwarded-User": "alpha-client", "X-Forwarded-Proto": "http",
			},
		},
	} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
			daemon.BaseURL()+"/workspaces/"+probe.workspace+"/v1/responses",
			strings.NewReader(`{"model":"chat","input":"forwarded authority check","stream":false}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+probe.credential)
		request.Header.Set("Content-Type", "application/json")
		for name, value := range probe.forwarded {
			request.Header.Set(name, value)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("forwarded headers changed Workspace authority for %s: status=%d want=%d", probe.workspace, response.StatusCode, http.StatusForbidden)
		}
	}
	if got := alphaCalls.Load(); got != 1 {
		t.Fatalf("alpha provider calls=%d want=1; forwarded authority must not bypass credential audience", got)
	}
	if got := betaCalls.Load(); got != 1 {
		t.Fatalf("beta provider calls=%d want=1; cross-Workspace credential must be denied before dispatch", got)
	}
}

func mustSlug(t *testing.T, raw string) routing.WorkspaceSlug {
	t.Helper()
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		t.Fatal(err)
	}
	return slug
}
