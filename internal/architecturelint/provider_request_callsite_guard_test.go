package architecturelint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderRequestConstructor_HasSingleProductionCallsite(t *testing.T) {
	t.Parallel()

	root := packageDirFromHere(t, "..")
	allowed := filepath.Join(root, "exchange", "runner.go")
	var hits []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(raw), "ports.NewProviderRequest(") {
			hits = append(hits, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal tree: %v", err)
	}
	if len(hits) != 1 || hits[0] != allowed {
		t.Fatalf("ports.NewProviderRequest production callsites=%v; want [%s]", hits, allowed)
	}
}
