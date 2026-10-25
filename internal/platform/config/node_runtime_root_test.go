package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestNodeRuntimeRootSeparatesExternalStateWithoutSplittingManagedState(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvSwobuHome, home)
	configA := filepath.Join(t.TempDir(), "swobu.yaml")
	configB := filepath.Join(t.TempDir(), "swobu.yaml")
	for _, path := range []string{configA, configB} {
		if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	managed, err := NodeRuntimeRoot(configA, false)
	if err != nil || managed != filepath.Dir(configA) {
		t.Fatalf("managed root=%q err=%v", managed, err)
	}
	a, err := NodeRuntimeRoot(configA, true)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := NodeRuntimeRoot(configA, true)
	if err != nil || repeated != a {
		t.Fatalf("unstable external root: %q vs %q (%v)", a, repeated, err)
	}
	b, err := NodeRuntimeRoot(configB, true)
	if err != nil || b == a {
		t.Fatalf("two different Nodes share runtime evidence: %q and %q (%v)", a, b, err)
	}
	if filepath.Dir(filepath.Dir(a)) != filepath.Join(home, "state") {
		t.Fatalf("external state escaped state root: %q", a)
	}
	t.Run("symlink alias", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "link.yaml")
		if err := os.Symlink(configA, alias); err != nil {
			if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
				t.Skip("Windows user cannot create symlinks; alias identity remains exercised on Unix")
			}
			t.Fatal(err)
		}
		aliased, err := NodeRuntimeRoot(alias, true)
		if err != nil || aliased != a {
			t.Fatalf("symlink alias split accounting state: %q vs %q (%v)", aliased, a, err)
		}
	})
}
