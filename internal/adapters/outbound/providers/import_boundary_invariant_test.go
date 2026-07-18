package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderPackages_DoNotImportProtocolFamilyPackagesDirectly(t *testing.T) {
	t.Parallel()

	root := "."
	forbidden := []string{
		`"github.com/swobuforge/swobu/internal/wire/chatcompletions"`,
		`"github.com/swobuforge/swobu/internal/wire/messages"`,
		`"github.com/swobuforge/swobu/internal/wire/responses"`,
	}
	allowedPathSnippets := []string{
		string(filepath.Separator) + "internal" + string(filepath.Separator) + "adapters" + string(filepath.Separator) + "wire" + string(filepath.Separator),
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		for _, allowed := range allowedPathSnippets {
			if strings.Contains(path, allowed) {
				return nil
			}
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content := string(raw)
		for _, disallowed := range forbidden {
			if strings.Contains(content, disallowed) {
				t.Fatalf("forbidden direct protocol-family import in %s: %s", path, disallowed)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk providers tree: %v", err)
	}
}
