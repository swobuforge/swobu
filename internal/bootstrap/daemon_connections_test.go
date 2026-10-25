package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestDaemonConnectionReadbackSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := connectionfixture.Open(filepath.Join(root, "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.New()
	endpoint, _ := connections.ParseEndpoint("https://example.com/mcp")
	spec, _ := connections.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:PRIVATE_LOCATOR"))
	value, err := connections.New(id, "Shared MCP", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		daemon, err := Start(t.Context(), StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer daemon.Close()
			response, err := http.Get(daemon.BaseURL() + "/api/v1/connections/" + id.String())
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			raw, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			// Decode scalar metadata independently: the spec sum is projected JSON,
			// not an editable request or persistence record.
			var metadata struct {
				ID, Revision string
				DisplayName  string `json:"display_name"`
			}
			if err := json.Unmarshal(raw, &metadata); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 200 || response.Header.Get("ETag") != `"1"` || metadata.ID != id.String() || metadata.DisplayName != "Shared MCP" || strings.Contains(string(raw), "PRIVATE_LOCATOR") {
				t.Fatalf("readback status=%d body=%s", response.StatusCode, raw)
			}
		}()
	}
}

func TestDaemonRejectsCorruptConnectionRegistry(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "swobu.yaml"), []byte("schema_version: 5\nworkspaces: {}\nconnections:\n  schema_version: 999\n  connections: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if daemon, err := Start(t.Context(), StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}}); err == nil {
		daemon.Close()
		t.Fatal("started with corrupt Connection registry")
	}
}

func TestDaemonConnectionAuditSurvivesDeletionAndRestartOnlyAtNodeScope(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := connectionfixture.Open(filepath.Join(root, "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := connections.ParseEndpoint("https://example.com/mcp")
	spec, _ := connections.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:PRIVATE_LOCATOR"))
	value, _, err := registry.CreateChecked(t.Context(), "PRIVATE_LABEL", spec, "local", "private-retry", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(root, "swobu.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := connections.NewLifecycle(registry, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.DeleteChecked(t.Context(), value.ID(), value.Revision(), "local", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := workspaces.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		daemon, err := Start(t.Context(), StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer daemon.Close()
			client := operatorclient.New(http.DefaultClient, daemon.BaseURL())
			node, err := client.NodeAuditEvents(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, event := range node.Events {
				if event.Connection != nil {
					count++
					if event.Connection.ConnectionID != value.ID().String() || event.Workspace != "" {
						t.Fatal("incorrect Connection scope/identity")
					}
				}
			}
			if count != 2 {
				t.Fatalf("missing retained Connection facts: %d", count)
			}
			workspace, err := client.AuditEvents(t.Context(), "research")
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range workspace.Events {
				if event.Connection != nil {
					t.Fatal("Workspace inherited Node Connection fact")
				}
			}
			raw, _ := json.Marshal(node)
			for _, secret := range []string{"PRIVATE_LABEL", "PRIVATE_LOCATOR", "private-retry", "example.com"} {
				if strings.Contains(string(raw), secret) {
					t.Fatalf("audit disclosed %s", secret)
				}
			}
		}()
	}
}

func TestDaemonBrowserConnectionDeleteAdmissionAndRestart(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SWOBU_HOME", filepath.Join(root, "home"))
	t.Setenv(config.EnvAuthCredentialWritePolicy, "file")
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := connectionfixture.Open(filepath.Join(root, "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.New()
	endpoint, _ := connections.ParseEndpoint("https://example.com/mcp")
	spec, _ := connections.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:PRIVATE_LOCATOR"))
	value, err := connections.New(id, "Shared", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	request := func(method, path, body, revision, origin string, browser bool) *http.Response {
		t.Helper()
		r, err := http.NewRequestWithContext(t.Context(), method, daemon.BaseURL()+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if browser {
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Set("Origin", origin)
		}
		if revision != "" {
			r.Header.Set("If-Match", revision)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	check := func(response *http.Response, status int) {
		t.Helper()
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("status=%d want=%d body=%s", response.StatusCode, status, body)
		}
	}
	path := "/api/v1/connections/" + id.String()
	check(request("DELETE", path, "", `"1"`, daemon.BaseURL(), true), 401)
	check(request("POST", path+"/checks", "", `"1"`, daemon.BaseURL(), true), 401)
	launch := request("POST", "/api/v1/browser-launches", "", "", "", false)
	var launched struct {
		Path string `json:"launch_path"`
	}
	if err := json.NewDecoder(launch.Body).Decode(&launched); err != nil {
		t.Fatal(err)
	}
	launch.Body.Close()
	check(request("POST", "/api/v1/browser-session", `{"token":"`+strings.TrimPrefix(launched.Path, "/open#")+`"}`, "", daemon.BaseURL(), true), 204)
	detail := request("GET", path, "", "", daemon.BaseURL(), true)
	revision := detail.Header.Get("ETag")
	check(detail, 200)
	check(request("POST", path+"/checks", "", revision, "https://other.example", true), 403)
	check(request("POST", path+"/checks", "", `"99"`, daemon.BaseURL(), true), 412)
	checked := request("POST", path+"/checks", "", revision, daemon.BaseURL(), true)
	var observation struct {
		ConnectionID string `json:"connection_id"`
		Revision     string `json:"connection_revision"`
		Error        string `json:"sanitized_error"`
	}
	if err := json.NewDecoder(checked.Body).Decode(&observation); err != nil {
		t.Fatal(err)
	}
	checked.Body.Close()
	if checked.StatusCode != 200 || observation.ConnectionID != id.String() || observation.Revision != "1" || observation.Error != "credential_unavailable" {
		t.Fatalf("check status=%d observation=%+v", checked.StatusCode, observation)
	}
	author := func(method, path, body, tag, key string) *http.Response {
		t.Helper()
		r, _ := http.NewRequestWithContext(t.Context(), method, daemon.BaseURL()+path, strings.NewReader(body))
		r.Header.Set("Origin", daemon.BaseURL())
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("Content-Type", "application/json")
		if tag != "" {
			r.Header.Set("If-Match", tag)
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	// Authoring must apply catalog policy through the mounted browser boundary,
	// before either create or edit can publish an invalid reusable identity.
	invalidModel := `{"display_name":"Invalid","kind":"model","spec":{"shape":"standard","provider_id":"openai","configuration":{"endpoint":"https://wrong.example/v1","credential_ref":"env:SYNTHETIC_TOKEN"}}}`
	check(author("POST", "/api/v1/connections", invalidModel, "", "invalid-provider-policy"), 400)
	check(author("PATCH", path, invalidModel, revision, ""), 400)
	draft := `{"display_name":"Authored","kind":"mcp","spec":{"transport":"stdio","configuration":{"executable":"node","args":["synthetic-private-arg"],"env_refs":{"TOKEN":"env:PRIVATE_AUTHORING"}}}}`
	created := author("POST", "/api/v1/connections", draft, "", "author-create")
	createdRaw, _ := io.ReadAll(created.Body)
	created.Body.Close()
	var createdResult struct {
		Connection struct{ ID, Revision string }
		Replayed   bool
	}
	if created.StatusCode != 201 || json.Unmarshal(createdRaw, &createdResult) != nil || createdResult.Connection.ID == "" || strings.Contains(string(createdRaw), "PRIVATE_AUTHORING") || strings.Contains(string(createdRaw), "synthetic-private-arg") {
		t.Fatalf("create: %d %s", created.StatusCode, createdRaw)
	}
	authoredPath := created.Header.Get("Location")
	check(author("POST", "/api/v1/connections", draft, "", "author-create"), 201)
	check(author("POST", "/api/v1/connections", strings.Replace(draft, "Authored", "Changed", 1), "", "author-create"), 409)
	edit := strings.Replace(draft, `"display_name":"Authored"`, `"display_name":"Edited","same_identity":true`, 1)
	check(author("PATCH", authoredPath, edit, `"1"`, ""), 200)
	check(author("PATCH", authoredPath, edit, `"1"`, ""), 412)
	check(author("DELETE", authoredPath, "", `"2"`, ""), 204)
	check(author("POST", "/api/v1/connections", draft, "", "author-create"), 201)
	check(author("GET", authoredPath, "", "", ""), 404)
	replacementPath := path + "/credential-replacements"
	replace := func(secret, key string, expected string) *http.Response {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"secret": secret, "same_identity": true})
		r, _ := http.NewRequestWithContext(t.Context(), "POST", daemon.BaseURL()+replacementPath, bytes.NewReader(body))
		r.Header.Set("Origin", daemon.BaseURL())
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", expected)
		r.Header.Set("Idempotency-Key", key)
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	check(request("POST", replacementPath, `{"secret":"synthetic-new","same_identity":true}`, revision, daemon.BaseURL(), true), 400)
	first := replace("synthetic-new", "replace", revision)
	replacementRevision := first.Header.Get("ETag")
	raw, _ := io.ReadAll(first.Body)
	first.Body.Close()
	if first.StatusCode != 200 || replacementRevision != `"2"` || strings.Contains(string(raw), "synthetic-new") || strings.Contains(string(raw), "secretfile:") {
		t.Fatalf("replacement status=%d body=%s", first.StatusCode, raw)
	}
	check(replace("synthetic-new", "replace", revision), 200)
	check(replace("synthetic-changed", "replace", revision), 409)
	check(replace("synthetic-new", "new-key", revision), 412)
	revision = replacementRevision
	check(request("DELETE", path, "", revision, "https://attacker.example", true), 403)
	check(request("DELETE", path, "", "", daemon.BaseURL(), true), 428)
	check(request("DELETE", path, "", `"1"`, daemon.BaseURL(), true), 412)
	check(request("DELETE", path, "", revision, daemon.BaseURL(), true), 204)
	check(request("DELETE", path, "", revision, daemon.BaseURL(), true), 404)
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Start(t.Context(), StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	response, err := http.Get(restarted.BaseURL() + path)
	if err != nil {
		t.Fatal(err)
	}
	check(response, 404)
	audit, err := operatorclient.New(http.DefaultClient, restarted.BaseURL()).NodeAuditEvents(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range audit.Events {
		if event.Connection != nil && event.Connection.ConnectionID == id.String() {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("delete audit facts=%d", count)
	}
}

// publishModelFixture uses the real registry publication seam before a fixture
// creates routing references. It never resolves the synthetic credential locator.
func publishModelFixture(t *testing.T, daemon *Daemon, draft routing.ConnectionDraft) string {
	t.Helper()
	upstream, err := routing.FinalizeConnection(draft, profile.RoutingConstructionFacts())
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	id, err := connectionidentity.New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, "Synthetic account", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.connectionStore.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// A clean managed Node is a single human-readable desired-state document for
// Workspaces and Connections. No test-only sidecar import or compatibility
// loader participates in this round trip.
func TestDaemonColdStartWithInlineConnectionAndWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	workspaces, err := configstore.OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := connections.OpenInline(nil, file.PublishConnections, profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	if err := workspaces.InstallNodePublisher(file.PublishRouting); err != nil {
		t.Fatal(err)
	}
	if _, err := connections.NewLifecycle(registry, workspaces); err != nil {
		t.Fatal(err)
	}
	id, err := connectionidentity.Parse("cn_inline")
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := routing.NewCustomConnection("custom", "https://example.test/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, "Managed inline Model", spec, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	slug, err := routing.ParseWorkspaceSlug("engineering")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.Update(t.Context(), func(current routing.Config) (routing.Config, error) {
		return current.CreateEmptyWorkspace(routing.EmptyWorkspaceSeed{Slug: slug})
	}); err != nil {
		t.Fatal(err)
	}
	if err := workspaces.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		daemon, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
		if err != nil {
			t.Fatal(err)
		}
		if daemon.configStore.Config().WorkspaceCount() != 1 || len(daemon.connectionStore.List()) != 1 {
			_ = daemon.Close()
			t.Fatal("Node did not reconstruct both typed sections")
		}
		res, err := http.Get(daemon.BaseURL() + "/api/v1/connections/" + id.String())
		if err != nil {
			_ = daemon.Close()
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			_ = daemon.Close()
			t.Fatalf("Connection readback status %d", res.StatusCode)
		}
		if err := daemon.Close(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("connections:")) || !bytes.Contains(raw, []byte("engineering:")) {
		t.Fatalf("Node file lost typed desired state: %s", raw)
	}
}
