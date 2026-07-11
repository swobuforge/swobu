package protocolregistry

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestProtocolPackagesDoNotDeclareFamilyWrapperTypes(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "families")
	matches, err := filepath.Glob(filepath.Join(root, "*", "client_*_*.go"))
	if err != nil {
		t.Fatalf("glob protocol codec files: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no protocol codec files found under %s", root)
	}

	wrapperTypePattern := regexp.MustCompile(`(?m)^type\s+\w+Family\s+struct\{\}\s*$`)
	for _, path := range matches {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if wrapperTypePattern.Match(body) {
			t.Fatalf("family wrapper type declaration is forbidden: %s", path)
		}
	}
}
