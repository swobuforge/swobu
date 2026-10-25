package configstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestStandaloneRoutingWriterCannotErasePrincipalAuthority(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	data := "schema_version: 5\nworkspaces: {}\nprincipals:\n  schema_version: 3\n  access: []\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, _ := routing.ParseWorkspaceSlug("engineering")
	if _, err := store.Update(t.Context(), func(config routing.Config) (routing.Config, error) {
		return config.CreateEmptyWorkspace(routing.EmptyWorkspaceSeed{Slug: workspace})
	}); err == nil {
		t.Fatal("standalone routing writer was allowed to discard Principals")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != data || !strings.Contains(string(after), "principals:") {
		t.Fatal("routing writer overwrote independent authority")
	}
}
