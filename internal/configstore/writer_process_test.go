package configstore

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestIndependentProcessCannotOpenOrMigrateAnActiveRoutingStore(t *testing.T) {
	// Re-execute this test binary, rather than sharing the parent's file
	// descriptor: the claim is exclusion of an independent filesystem writer.
	if path := os.Getenv("SWOBU_ROUTING_LOCK_TEST_PATH"); path != "" {
		if _, err := Open(path); !errors.Is(err, ErrLocked) {
			t.Fatalf("second process Open: %v", err)
		}
		if _, err := OpenOrCreateAfterRecovery(path, func() error { t.Fatal("locked recovery callback ran"); return nil }); !errors.Is(err, ErrLocked) {
			t.Fatalf("second process migration: %v", err)
		}
		return
	}
	for _, schema := range []string{"5"} {
		t.Run("schema="+schema, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "swobu.yaml")
			if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			raw := []byte("schema_version: " + schema + "\nworkspaces: {}\n")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), binary, "-test.run=^TestIndependentProcessCannotOpenOrMigrateAnActiveRoutingStore$", "-test.timeout=20s")
			command.Env = append(os.Environ(), "SWOBU_ROUTING_LOCK_TEST_PATH="+path)
			command.WaitDelay = 5 * time.Second
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("independent writer probe: %v\n%s", err, output)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(raw, after) {
				t.Fatalf("active config changed: %v", err)
			}
			if _, err := os.Stat(path + ".v1.bak"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("denied writer created migration backup: %v", err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			// The same path is usable after ownership release, so denial was
			// the lock contract rather than malformed configuration or permissions.
			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if reopened.schemaVersion != 5 {
				t.Fatal("released path did not reopen as schema 5")
			}
		})
	}
}
