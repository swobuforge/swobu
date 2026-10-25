//go:build windows

package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
	"github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/configstore"
)

// Windows local state uses ordinary OS permissions, without ACL inspection
// or rewrite at startup.
func TestWindowsLocalStateUsesHostPermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "node")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	config, err := configstore.OpenOrCreate(filepath.Join(root, "swobu.yaml"))
	if err != nil {
		t.Fatalf("native config/lock admission: %v", err)
	}
	if err := config.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"connect-attempts", "auth-start-attempts"} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if name == "connect-attempts" {
			if _, err := connect.OpenAttempts(path); err != nil {
				t.Fatalf("native Connect admission: %v", err)
			}
		} else if _, err := authplane.OpenStartAttempts(path); err != nil {
			t.Fatalf("native auth-start admission: %v", err)
		}
	}
}
