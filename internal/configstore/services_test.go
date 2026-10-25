package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestServiceLifecycleYAMLAndCheckedPublication(t *testing.T) {
	store, path := openTestStore(t)
	slug, _ := routing.ParseWorkspaceSlug("research")
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		return c.CreateEmptyWorkspace(routing.EmptyWorkspaceSeed{Slug: slug})
	}); err != nil {
		t.Fatal(err)
	}
	initial, _ := store.Config().Workspace(slug)
	etag := fmt.Sprintf("%s-%d", initial.ManagementIncarnation(), initial.ManagementRevision())
	admitCount := 0
	admit := func(context.Context) error { admitCount++; return nil }
	enabled, err := store.SetWorkspaceServiceChecked(t.Context(), slug, routing.ServiceMCP, true, etag, admit)
	if err != nil || !enabled.HasService(routing.ServiceMCP) {
		t.Fatalf("enable MCP: %v", err)
	}
	if admitCount != 1 {
		t.Fatal("admission not checked")
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(raw, []byte("enabled_services:")) || !bytes.Contains(raw, []byte("- mcp")) {
		t.Fatalf("enabled-empty not persisted: %s, %v", raw, err)
	}
	reloaded, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	fromDisk, _ := reloaded.Config().Workspace(slug)
	if !fromDisk.HasService(routing.ServiceMCP) || len(fromDisk.MCPMounts()) != 0 {
		t.Fatal("read-only reopen lost enabled-empty MCP")
	}
	afterEnable, _ := os.ReadFile(path)
	if _, err := store.SetWorkspaceServiceChecked(t.Context(), slug, routing.ServiceMCP, false, etag, admit); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale etag: %v", err)
	}
	if _, err := store.SetWorkspaceServiceChecked(t.Context(), slug, routing.ServiceMCP, false, fmt.Sprintf("%s-%d", enabled.ManagementIncarnation(), enabled.ManagementRevision()), func(context.Context) error { return errors.New("no admission") }); err == nil {
		t.Fatal("unadmitted edit succeeded")
	}
	afterRejected, _ := os.ReadFile(path)
	if !bytes.Equal(afterEnable, afterRejected) {
		t.Fatal("stale/denied edit changed YAML")
	}
	// A repeated enable is a semantic no-op: no new ETag or YAML rewrite.
	noOp, err := store.SetWorkspaceServiceChecked(t.Context(), slug, routing.ServiceMCP, true, fmt.Sprintf("%s-%d", enabled.ManagementIncarnation(), enabled.ManagementRevision()), admit)
	if err != nil || noOp.ManagementRevision() != enabled.ManagementRevision() {
		t.Fatalf("no-op advanced revision: %v", err)
	}
	afterNoOp, _ := os.ReadFile(path)
	if !bytes.Equal(afterEnable, afterNoOp) {
		t.Fatal("no-op rewrote file")
	}
}

func TestExplicitServiceYAMLValidatesAbsentAndUnknownKinds(t *testing.T) {
	for name, content := range map[string]string{
		"disabled models": "    enabled_services: []\n    routes:\n      - pattern: chat\n        tiers:\n          - targets:\n              - id: x\n                version: 1\n                model: gpt-5\n                protocol: responses\n                connection_id: cn_shared\n",
		"unreleased":      "    enabled_services: [images]\n    routes: []\n",
		"duplicate":       "    enabled_services: [models, models]\n    routes: []\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "desired.yaml")
			raw := []byte("schema_version: 5\nworkspaces:\n  research:\n" + content)
			if err := os.WriteFile(path, raw, 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenReadOnly(path); err == nil {
				t.Fatalf("invalid Services accepted: %s", raw)
			}
		})
	}
	// Omitted enabled_services in the current schema derives only configured services.
	old := "schema_version: 5\nworkspaces:\n  research:\n    routes: []\n    search:\n      connection_id: cn_search\n"
	config, err := decode([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	w, _ := config.Workspace(slug)
	if got := w.EnabledServices(); len(got) != 1 || got[0] != routing.ServiceSearch {
		t.Fatalf("legacy inference: %v", got)
	}
	persisted, err := encode(config)
	if err != nil || !strings.Contains(string(persisted), "enabled_services:") {
		t.Fatalf("explicit serialize: %s %v", persisted, err)
	}
}

func TestReadOnlyServiceMutationsDoNotCallAdmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desired.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0444); err != nil {
		t.Fatal(err)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	slug, _ := routing.ParseWorkspaceSlug("research")
	_, err = store.SetWorkspaceServiceChecked(context.Background(), slug, routing.ServiceMCP, true, "etag", func(context.Context) error { t.Fatal("admission executed"); return nil })
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("read-only effect: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only created lock")
	}
}
