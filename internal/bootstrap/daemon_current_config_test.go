package bootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenDaemonRoutingDoesNotRewriteLegacyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("schema_version: 1\nworkspaces: {}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := openDaemonRouting(t.Context(), path)
	if store != nil {
		_ = store.Close()
		t.Fatal("legacy routing document opened")
	}
	if err == nil || !strings.Contains(err.Error(), "migration is disabled") {
		t.Fatalf("err=%v, want explicit migration-disabled failure", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || !bytesEqual(got, original) {
		t.Fatalf("legacy document changed: read=%v bytes_equal=%v", readErr, bytesEqual(got, original))
	}
	if _, statErr := os.Stat(path + ".lock"); statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
