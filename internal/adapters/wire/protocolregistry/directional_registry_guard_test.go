package protocolregistry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRegistryDoesNotExportSplitClientOrProviderResponseLookups(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(filename), "registry.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse registry.go: %v", err)
	}

	forbidden := map[string]struct{}{
		"ForClientRequestFamily":              {},
		"ForClientResponseDocumentFamily":     {},
		"ForClientResponseStreamFamily":       {},
		"ForProviderResponseDocumentProtocol": {},
		"ForProviderResponseStreamProtocol":   {},
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		if _, hit := forbidden[fn.Name.Name]; hit {
			t.Fatalf("forbidden exported split lookup function remains: %s", fn.Name.Name)
		}
	}
}
