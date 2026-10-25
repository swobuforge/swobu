package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/bootstrap"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/nodefile"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/profile"
)

type deletionConfirmationReader struct {
	t          *testing.T
	notices    *bytes.Buffer
	input      string
	beforeRead func()
}

type deletionFaultTransport struct {
	next  http.RoundTripper
	fault string
}

func publishDeleteFixtureConnection(t *testing.T, configPath string) {
	t.Helper()
	store, err := openInlineDeleteFixtureRegistry(t, configPath)
	if err != nil {
		t.Fatal(err)
	}
	id, err := connectionidentity.Parse("cn_fixture_docs")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := connections.ParseEndpoint("https://fixture.example.test/mcp")
	if err != nil {
		t.Fatal(err)
	}
	// Deletion tests need durable references, not a live MCP transport.
	// Fail credential resolution locally before the fictional endpoint is used.
	t.Setenv("SWOBU_MISSING_DELETE_MCP_FIXTURE", "")
	spec, err := connections.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:SWOBU_MISSING_DELETE_MCP_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, "Fixture docs", spec, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	searchID, err := connectionidentity.Parse("cn_fixture_search")
	if err != nil {
		t.Fatal(err)
	}
	search, err := connections.NewSearchSpec("fixture", endpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	searchValue, err := connections.New(searchID, "Fixture search", search, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(searchValue, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
}

func (t deletionFaultTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.fault == "before-apply" && r.Method == http.MethodDelete && r.URL.RawQuery == "" {
		return nil, errors.New("connection lost before apply")
	}
	if t.fault == "readback" && r.Method == http.MethodGet && r.URL.Path == "/api/v1/workspaces/research" {
		return nil, errors.New("readback unavailable")
	}
	resp, err := t.next.RoundTrip(r)
	if err == nil && t.fault == "lost-response" && r.Method == http.MethodDelete && r.URL.RawQuery == "" {
		resp.Body.Close()
		return nil, errors.New("response lost after commit")
	}
	return resp, err
}

func (r *deletionConfirmationReader) Read(p []byte) (int, error) {
	if (!strings.Contains(r.notices.String(), "does not restore those credentials") && !strings.Contains(r.notices.String(), "independently durable")) || !strings.Contains(r.notices.String(), "ETag:") || !strings.Contains(r.notices.String(), "DELETE research") {
		r.t.Fatal("confirmation requested before exact server preview")
	}
	if r.beforeRead != nil {
		r.beforeRead()
		r.beforeRead = nil
	}
	if r.input == "" {
		return 0, io.EOF
	}
	n := copy(p, r.input)
	r.input = r.input[n:]
	return n, nil
}

func TestWorkspaceDeleteRealDaemonTrustedConfirmation(t *testing.T) {
	for _, variant := range []string{"approve", "decline", "stale", "noninteractive", "readback", "lost-response", "lost-response-recreated", "before-apply"} {
		t.Run(variant, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv(platformconfig.EnvSwobuHome, dir)
			path := filepath.Join(dir, "swobu.yaml")
			if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			publishDeleteFixtureConnection(t, path)
			daemon, err := bootstrap.Start(t.Context(), bootstrap.StartInput{ConfigPath: path, StartupConfig: platformconfig.StartupConfig{Addr: "127.0.0.1:0"}})
			if err != nil {
				t.Fatal(err)
			}
			defer daemon.Close()
			api := operatorclient.New(&http.Client{}, daemon.BaseURL())
			create, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/api/v1/workspaces", strings.NewReader(`{"slug":"research"}`))
			if err != nil {
				t.Fatal(err)
			}
			create.Header.Set("Content-Type", "application/json")
			create.Header.Set("Idempotency-Key", "deletion-fixture-creation")
			created, err := http.DefaultClient.Do(create)
			if err != nil {
				t.Fatal(err)
			}
			createdBody, err := io.ReadAll(created.Body)
			created.Body.Close()
			if created.StatusCode != 201 {
				t.Fatalf("create: %d", created.StatusCode)
			}
			var createdProjection struct {
				ManagementETag string `json:"management_etag"`
			}
			if err := json.Unmarshal(createdBody, &createdProjection); err != nil {
				t.Fatal(err)
			}
			enable, err := http.NewRequestWithContext(t.Context(), http.MethodPut, daemon.BaseURL()+"/api/v1/workspaces/research/services/search", nil)
			if err != nil {
				t.Fatal(err)
			}
			enable.Header.Set("If-Match", `"`+createdProjection.ManagementETag+`"`)
			enableResponse, err := http.DefaultClient.Do(enable)
			if err != nil {
				t.Fatal(err)
			}
			enableResponse.Body.Close()
			if enableResponse.StatusCode != http.StatusOK {
				t.Fatalf("enable Search: %d", enableResponse.StatusCode)
			}
			if etag := strings.Trim(enableResponse.Header.Get("ETag"), `"`); etag != "" {
				createdProjection.ManagementETag = etag
			}
			var out, notices bytes.Buffer
			input := &deletionConfirmationReader{t: t, notices: &notices, input: "DELETE research\n"}
			if variant == "decline" {
				input.input = "APPLY\n"
			}
			if variant == "stale" {
				input.beforeRead = func() {
					req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, daemon.BaseURL()+"/api/v1/workspaces/research/search-route", strings.NewReader(`{"connection_id":"cn_fixture_search","fallback_connection_ids":[]}`))
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("If-Match", `"`+createdProjection.ManagementETag+`"`)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					if resp.StatusCode != 200 {
						t.Fatalf("stale edit: %d", resp.StatusCode)
					}
				}
			}
			fault := variant
			if variant == "lost-response-recreated" {
				fault = "lost-response"
			}
			runner := Runner{Stdin: input, Stdout: &out, Stderr: &notices, HTTPClient: &http.Client{Transport: deletionFaultTransport{next: http.DefaultTransport, fault: fault}}, IsInteractive: func() bool { return variant != "noninteractive" }}
			code := runner.Run(t.Context(), []string{"workspace", "delete", "--workspace", "research", "--addr", strings.TrimPrefix(daemon.BaseURL(), "http://")})
			wantDeleted := variant == "approve" || variant == "readback" || strings.HasPrefix(variant, "lost-response")
			_, err = api.GetWorkspace(t.Context(), "research")
			if (err != nil) != wantDeleted || (code == ExitHealthy) != (variant == "approve") {
				t.Fatalf("code=%d read=%v out=%s notices=%s", code, err, out.String(), notices.String())
			}
			if (variant == "readback" || variant == "lost-response") && !strings.Contains(notices.String(), "rollback") {
				t.Fatal("uncertain outcome implied rollback")
			}
			if strings.HasPrefix(variant, "lost-response") || variant == "before-apply" {
				var key, etag string
				for _, line := range strings.Split(notices.String(), "\n") {
					if strings.HasPrefix(line, "Retry key: ") {
						key = strings.TrimPrefix(line, "Retry key: ")
					}
					if strings.HasPrefix(line, "ETag: ") {
						etag = strings.TrimPrefix(line, "ETag: ")
					}
				}
				if key == "" || etag == "" {
					t.Fatal("original retry intent unavailable")
				}
				if variant == "lost-response-recreated" {
					recreate, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/api/v1/workspaces", strings.NewReader(`{"slug":"research"}`))
					if err != nil {
						t.Fatal(err)
					}
					recreate.Header.Set("Content-Type", "application/json")
					recreate.Header.Set("Idempotency-Key", "deletion-fixture-recreation")
					resp, err := http.DefaultClient.Do(recreate)
					if err != nil {
						t.Fatal(err)
					}
					resp.Body.Close()
					if resp.StatusCode != 201 {
						t.Fatalf("recreate=%d", resp.StatusCode)
					}
				}
				args := []string{"workspace", "delete", "--workspace", "research", "--addr", strings.TrimPrefix(daemon.BaseURL(), "http://"), "--retry-key", key, "--expected-etag", etag}
				runner.HTTPClient = &http.Client{}
				notices.Reset()
				input.input = "no\n"
				if code := runner.Run(t.Context(), args); code != ExitDown || !strings.Contains(notices.String(), "no apply was sent") {
					t.Fatal("declined recovery applied")
				}
				if variant != "before-apply" {
					wrongIntent := append([]string(nil), args...)
					wrongIntent[len(wrongIntent)-1] = etag + "changed"
					notices.Reset()
					input.input = "DELETE research\n"
					if code := runner.Run(t.Context(), wrongIntent); code != ExitDown || !strings.Contains(notices.String(), "idempotency_conflict") {
						t.Fatalf("changed recovery intent not rejected: %d %s", code, notices.String())
					}
				}
				notices.Reset()
				out.Reset()
				input.input = "DELETE research\n"
				code = runner.Run(t.Context(), args)
				_, err = api.GetWorkspace(t.Context(), "research")
				if (variant == "lost-response" || variant == "before-apply") && (code != ExitHealthy || !operatorclient.IsNotFound(err)) {
					t.Fatalf("recovery failed: %d %v %s", code, err, notices.String())
				}
				if variant == "lost-response-recreated" && (code != ExitDown || err != nil || !strings.Contains(notices.String(), "changed lifetime/revision")) {
					t.Fatalf("recovery touched replacement: %d %v %s", code, err, notices.String())
				}
				if !strings.Contains(notices.String(), "No current ETag will be substituted") {
					t.Fatal("recovery consent hid intent binding")
				}
			}
		})
	}
}

func TestWorkspaceDeletionRecoveryRejectsIncompleteOrUnsafeIntent(t *testing.T) {
	for _, flags := range [][]string{{"--retry-key", "key"}, {"--expected-etag", "etag"}, {"--retry-key", "key", "--expected-etag", "\x1b[2J"}, {"--retry-key", strings.Repeat("k", 257), "--expected-etag", "etag"}} {
		var out, notices bytes.Buffer
		runner := Runner{Stdin: strings.NewReader("DELETE research\n"), Stdout: &out, Stderr: &notices, IsInteractive: func() bool { return true }}
		if code := runner.Run(t.Context(), append([]string{"workspace", "delete", "--workspace", "research"}, flags...)); code != ExitDown {
			t.Fatalf("invalid recovery allowed: %v", flags)
		}
	}
}

func openInlineDeleteFixtureRegistry(t *testing.T, configPath string) (*connections.Store, error) {
	t.Helper()
	managed, err := configstore.OpenOrCreate(configPath)
	if err != nil {
		return nil, err
	}
	if err := managed.Close(); err != nil {
		return nil, err
	}
	node, err := nodefile.Open(configPath)
	if err != nil {
		return nil, err
	}
	raw, err := node.ConnectionDocument()
	if err != nil {
		return nil, err
	}
	return connections.OpenInline(raw, node.PublishConnections, profile.SupportsSpec)
}
