package views

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPickerConsistency_NoDirectChoiceOptionHelperUsageInAppViews(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	rootDir := filepath.Dir(thisFile)
	forbidden := []string{
		"NewChoiceOption[",
		"NewChoiceOptionWithCancel[",
	}

	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(content)
		for _, token := range forbidden {
			if strings.Contains(text, token) {
				t.Fatalf("forbidden picker helper %q found in %s; use RenderFilterablePickerDisclosure + ChoicePickerOptionRow/ListItemRow instead", token, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk app views: %v", err)
	}
}
