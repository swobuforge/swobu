package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func seedTestWorkspaces(t *testing.T, configPath string, names ...string) {
	t.Helper()
	content := "schema_version: 5\nworkspaces:\n"
	for _, name := range names {
		if _, err := routing.ParseWorkspaceSlug(name); err != nil {
			t.Fatal(err)
		}
		content += "  " + name + ":\n    routes: []\n"
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
