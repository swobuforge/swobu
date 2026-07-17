package help

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpPageDoesNotImportPlatformEffects(t *testing.T) {
	root := t.TempDir()
	// Copy all .go files from this package into a temp tree so we can parse
	// the full package without importing the test file itself.
	srcDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(root, e.Name()), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", e.Name(), err)
		}
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, root, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse dir: %v", err)
	}

	forbidden := []string{
		"github.com/swobuforge/swobu/internal/platform/browser",
		"github.com/swobuforge/swobu/internal/platform/clipboard",
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				for _, f := range forbidden {
					if path == f {
						t.Fatalf("help page imports forbidden platform package %q", path)
					}
				}
			}
		}
	}
}

func TestHelpPageUsesEffectAbstractions(t *testing.T) {
	// Verify the generated help page source references the reusable UI
	// components instead of direct platform calls.
	srcDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(srcDir, "page_view_gsx.go"))
	if err != nil {
		t.Fatalf("read generated gsx: %v", err)
	}
	got := string(data)

	for _, want := range []string{
		"ui.LinkRowComponent",
		"ui.CopyPasteRowComponent",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated help page missing expected effect abstraction %q", want)
		}
	}

	bad := []string{
		"browser.Open",
		"clipboard.TryWriteText",
		"clipboard.WriteTempFileFallback",
	}
	for _, b := range bad {
		if strings.Contains(got, b) {
			t.Fatalf("generated help page still contains direct platform call %q", b)
		}
	}
}
