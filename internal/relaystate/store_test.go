package relaystate

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRelayIdentityPersistsWithRestrictiveCreationDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	first, err := store.EndpointID()
	if err != nil || first == "" {
		t.Fatal(first, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("relay identity file creation mode = %04o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, obsolete := range [][]byte{[]byte("grants"), []byte("bearers"), []byte("anonymous"), []byte("exposures")} {
		if bytes.Contains(raw, obsolete) {
			t.Fatalf("obsolete authority leaked into relay TLS identity: %q", obsolete)
		}
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.EndpointID()
	if err != nil || got != first {
		t.Fatalf("relay identity not stable: %s vs %s: %v", got, first, err)
	}
}

// Existing OS permissions are the local operator's choice. Reopening an
// existing relay key must not rewrite its mode or rotate its identity.
func TestRelayOpenPreservesExistingHostPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	identity, err := store.EndpointID()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("valid relay state rejected based on OS mode: %v", err)
	}
	after, err := reopened.EndpointID()
	if err != nil || identity != after {
		t.Fatal("identity rotated", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0644 {
		t.Fatalf("existing permissions rewritten: %o", info.Mode().Perm())
	}
}

func TestOldShareStateIsNotAcceptedAsRelayIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":2,"endpoint":{},"grants":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("obsolete Share authority was accepted into relay identity")
	}
}
