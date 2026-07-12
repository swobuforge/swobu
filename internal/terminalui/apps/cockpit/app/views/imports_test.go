package views

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCockpitAppViews_DoNotImportRendergraphDirectly(t *testing.T) {
	t.Parallel()

	root := packageDir(t)
	var goFiles []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Keep this invariant on production files; test fixtures may still need
		// direct layout helpers while the migration is in flight.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		goFiles = append(goFiles, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	for _, path := range goFiles {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("relative path for %s: %v", path, err)
		}

		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		for _, spec := range parsed.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			if strings.HasPrefix(importPath, "github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph") {
				t.Fatalf("%s imports forbidden rendergraph package %q", rel, importPath)
			}
		}
	}
}

func packageDir(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}
