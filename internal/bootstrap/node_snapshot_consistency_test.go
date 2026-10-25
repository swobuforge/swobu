package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/configstore"
)

func TestBootstrapRejectsMixedWorkspaceAndNodeFileGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	original := []byte("schema_version: 5\nworkspaces: {}\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	routing, err := configstore.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer routing.Close()
	if len(routing.OpenedNodeRevision()) != 64 {
		t.Fatal("missing captured startup digest")
	}
	// Simulate a GitOps atomic file publication between the two bootstrap
	// readers. This is valid YAML with unchanged semantics but DIFFERENT bytes;
	// it must fail before any new authority or runtime side effects are wired.
	if err := os.WriteFile(path, append(original, []byte("# external replace\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = openDaemonConnections(context.Background(), nil, path, routing)
	if err == nil || !strings.Contains(err.Error(), "changed between Workspace and Node") {
		t.Fatalf("mixed snapshots accepted: %v", err)
	}
}
