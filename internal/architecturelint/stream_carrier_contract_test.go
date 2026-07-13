package architecturelint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStreamDecodeHelpers_UseWireStreamCarrier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		rel  string
	}{
		{name: "chatcompletions", rel: filepath.Join("..", "adapters", "wire", "families", "chatcompletions", "response_decode.go")},
		{name: "completions", rel: filepath.Join("..", "adapters", "wire", "families", "completions", "response_decode.go")},
		{name: "messages", rel: filepath.Join("..", "adapters", "wire", "families", "messages", "response_stream.go")},
		{name: "responses", rel: filepath.Join("..", "adapters", "wire", "families", "responses", "response_stream.go")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := fromHere(t, tc.rel)
			assertDecodeResponseStreamUsesWireStream(t, path)
		})
	}
}

func assertDecodeResponseStreamUsesWireStream(t *testing.T, filePath string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", filePath, err)
	}

	var found bool
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "decodeResponseStream" {
			return true
		}
		found = true
		if fn.Type == nil || fn.Type.Params == nil || len(fn.Type.Params.List) < 1 {
			t.Fatalf("%s decodeResponseStream must have parameters", filePath)
		}
		first := fn.Type.Params.List[0]
		sel, ok := first.Type.(*ast.SelectorExpr)
		if !ok {
			t.Fatalf("%s decodeResponseStream first param must be carrier.WireStream", filePath)
		}
		xIdent, ok := sel.X.(*ast.Ident)
		if !ok || xIdent.Name != "carrier" || sel.Sel == nil || sel.Sel.Name != "WireStream" {
			t.Fatalf("%s decodeResponseStream first param must be carrier.WireStream", filePath)
		}
		return false
	})
	if !found {
		t.Fatalf("%s missing decodeResponseStream helper", filePath)
	}
}

func fromHere(t *testing.T, rel string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	base := filepath.Dir(filename)
	path := filepath.Clean(filepath.Join(base, rel))
	if path == "" {
		t.Fatal(fmt.Errorf("invalid path from rel=%q", rel))
	}
	return path
}
