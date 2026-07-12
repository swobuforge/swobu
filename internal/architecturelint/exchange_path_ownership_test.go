package architecturelint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestExchangePathDoesNotImportRequestpath(t *testing.T) {
	root := packageDirFromHere(t, "..")
	targets := []string{
		filepath.Join(root, "adapters", "inbound", "httpapi"),
		filepath.Join(root, "bootstrap"),
	}
	for _, dir := range targets {
		entries, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		for _, file := range entries {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			assertNoRequestpathImport(t, file)
		}
	}
}

func assertNoRequestpathImport(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, imp := range node.Imports {
		lit := strings.TrimSpace(strings.Trim(imp.Path.Value, `"`)) // swobu:io-string source=domain
		if strings.HasSuffix(lit, "/internal/app/requestpath") {
			t.Fatalf("%s imports forbidden package %q", path, lit)
		}
	}
	ast.Inspect(node, func(ast.Node) bool { return false })
}
