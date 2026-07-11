package architecturelint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionCodeDoesNotUseEscapeHatchSymbols(t *testing.T) {
	t.Parallel()

	root := fromHere(t, "..")
	forbidden := []string{
		strings.Join([]string{"Wire", "Pa", "tch"}, ""),
		strings.Join([]string{"Request", "Patcher"}, ""),
		"ApplyEncode",
		"ApplyDecode",
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "testdata" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(raw)
		for _, token := range forbidden {
			if strings.Contains(text, token) {
				t.Fatalf("forbidden escape-hatch symbol %q found in production file %s", token, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal root: %v", err)
	}
}
