package bootstrap

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/platform/config"
)

// Admission is not just a managed write invariant. Externally managed YAML
// must fail closed before starting listeners if a budget references no Workspace.
func TestStartupRejectsDanglingAccountingPolicyWithoutChangingNode(t *testing.T) {
	for _, readOnly := range []bool{true, false} {
		t.Run(map[bool]string{true: "external", false: "managed"}[readOnly], func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "swobu.yaml")
			original := []byte(`schema_version: 5
workspaces: {}
accounting:
  schema_version: 1
  limits:
    orphan:
      currency: USD
      amount_cents: 200
      window: day
      mode: stop_new_billable_work
      unknown_price: reject
`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			daemon, err := Start(context.Background(), StartInput{
				ConfigPath: path, ReadOnlyConfig: readOnly,
				StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
			})
			if err == nil {
				_ = daemon.Close()
				t.Fatal("accepted unowned accounting policy")
			}
			if !strings.Contains(err.Error(), "accounting.limits.orphan") {
				t.Fatal(err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(original, after) {
				t.Fatal("invalid config was changed", readErr)
			}
		})
	}
}
