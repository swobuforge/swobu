package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/nodefile"
)

func TestConfigValidateIsOfflineAndSideEffectFree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "swobu.yaml")
	yaml := "schema_version: 5\nworkspaces: {}\n"
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	beforeMode, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// No adjacent registry is necessary for an empty Node.
	var out, errs bytes.Buffer
	runner := Runner{Stdout: &out, Stderr: &errs}
	if code := runner.Run(context.Background(), []string{"config", "validate", "--config", path}); code != ExitHealthy {
		t.Fatalf("empty valid code=%d stderr=%s", code, errs.String())
	}
	if !strings.Contains(out.String(), "Valid inline Node authority") {
		t.Fatal(out.String())
	}
	for _, name := range []string{"connections.json", "swobu.yaml.lock", "access.json", "relay.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected state file %s: %v", name, err)
		}
	}
	afterMode, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if beforeMode.Mode().Perm() != afterMode.Mode().Perm() {
		t.Fatal("offline validate changed permissions")
	}
	afterBytes, err := os.ReadFile(path)
	if err != nil || string(afterBytes) != yaml {
		t.Fatal("offline validate changed config bytes", err)
	}
	out.Reset()
	errs.Reset()
	if code := runner.Run(context.Background(), []string{"config", "validate", "--config", path, "unexpected"}); code == ExitHealthy {
		t.Fatal("ignored stray argument")
	}
	out.Reset()
	errs.Reset()
	if code := runner.Run(context.Background(), []string{"config", "validate", "--config", filepath.Join(dir, "missing.yaml")}); code == ExitHealthy {
		t.Fatal("missing file accepted")
	}
}

func TestConfigValidateGraphRejectsAbsentAndWrongKindConnections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "swobu.yaml")
	document := "schema_version: 5\nworkspaces:\n  engineering:\n    routes: []\n    mcp_mounts:\n      github:\n        connection_id: cn_upstream\n        allowed_tools: [search_code]\n"
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	runner := Runner{Stdout: &out, Stderr: &errs}
	check := func(success bool) {
		t.Helper()
		out.Reset()
		errs.Reset()
		code := runner.Run(context.Background(), []string{"config", "validate", "--config", path})
		if (code == ExitHealthy) != success {
			t.Fatalf("code=%d err=%s", code, errs.String())
		}
	}
	check(false)

	id, err := connectionidentity.Parse("cn_upstream")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := connections.ParseEndpoint("https://search.example")
	if err != nil {
		t.Fatal(err)
	}
	wrongKind, err := connections.NewSearchSpec("brave", endpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := openInlineConfigTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := connections.New(id, "Search Connection", wrongKind, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(wrong, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestConfigValidateAcceptsCurrentMCPAndSearchConnections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "swobu.yaml")
	input := `schema_version: 5
workspaces:
  engineering:
    routes: []
    mcp_mounts:
      github:
        connection_id: cn_mcp
        allowed_tools: [search_code]
    search:
      connection_id: cn_search
`
	if err := os.WriteFile(path, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	registry, err := openInlineConfigTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ep, err := connections.ParseEndpoint("https://example.test/upstream")
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := connections.NewMCPHTTPSpec(ep, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	search, err := connections.NewSearchSpec("brave", ep, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []struct {
		id, name string
		spec     connections.Spec
	}{{"cn_mcp", "MCP", mcp}, {"cn_search", "Search", search}} {
		id, err := connectionidentity.Parse(cfg.id)
		if err != nil {
			t.Fatal(err)
		}
		value, err := connections.New(id, cfg.name, cfg.spec, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if err := registry.Publish(value, connections.Revision{}); err != nil {
			t.Fatal(err)
		}
	}
	var out, errs bytes.Buffer
	runner := Runner{Stdout: &out, Stderr: &errs}
	if code := runner.Run(context.Background(), []string{"config", "validate", "--config", path}); code != ExitHealthy {
		t.Fatalf("current topology failed: %d %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "2 Connections") {
		t.Fatal(out.String())
	}

	corrupted := strings.Replace(input, "connection_id: cn_search", "connection_id: search-registry", 1)
	if err := os.WriteFile(path, []byte(corrupted), 0644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errs.Reset()
	if code := runner.Run(context.Background(), []string{"config", "validate", "--config", path}); code == ExitHealthy {
		t.Fatalf("legacy Search identity accepted: %s", out.String())
	}
}

func openInlineConfigTestStore(path string) (*connections.Store, error) {
	file, err := nodefile.Open(path)
	if err != nil {
		return nil, err
	}
	raw, err := file.ConnectionDocument()
	if err != nil {
		return nil, err
	}
	return connections.OpenInline(raw, file.PublishConnections, nil)
}
