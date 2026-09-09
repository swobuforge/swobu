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
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
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
