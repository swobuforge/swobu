package architecturelint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestProtocolFamilies_DecodeProviderStream_ValidatesWireStreamInvariant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		rel  string
	}{
		{name: "chatcompletions", rel: filepath.Join("..", "adapters", "wire", "families", "chatcompletions", "client_request_decoder.go")},
		{name: "completions", rel: filepath.Join("..", "adapters", "wire", "families", "completions", "client_request_decoder.go")},
		{name: "messages", rel: filepath.Join("..", "adapters", "wire", "families", "messages", "client_document_encoder.go")},
		{name: "responses", rel: filepath.Join("..", "adapters", "wire", "families", "responses", "client_document_encoder.go")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := fromHere(t, tc.rel)
			assertDecodeStreamEntryCallsWireStreamValidator(t, path)
			assertDecodeStreamEntryCarriesInvariantDetails(t, path)
			assertDecodeStreamEntryDetailUsesInvariantError(t, path)
		})
	}
}

func assertDecodeStreamEntryCallsWireStreamValidator(t *testing.T, filePath string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", filePath, err)
	}

	var foundFunc bool
	var foundValidateCall bool
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "DecodeProviderStream" {
			return true
		}
		foundFunc = true
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "ValidateResponseSSECarrierStream" {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if ok && x.Name == "core" {
				foundValidateCall = true
				return false
			}
			return true
		})
		return false
	})

	if !foundFunc {
		t.Fatalf("%s missing DecodeProviderStream", filePath)
	}
	if !foundValidateCall {
		t.Fatalf("%s DecodeProviderStream must call core.ValidateResponseSSECarrierStream", filePath)
	}
}

func assertDecodeStreamEntryCarriesInvariantDetails(t *testing.T, filePath string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", filePath, err)
	}

	var foundFunc bool
	var foundInvariantDetailKey bool
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "DecodeProviderStream" {
			return true
		}
		foundFunc = true
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			lit, ok := inner.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if lit.Value == "\"wire_stream_invariant\"" {
				foundInvariantDetailKey = true
				return false
			}
			return true
		})
		return false
	})

	if !foundFunc {
		t.Fatalf("%s missing DecodeProviderStream", filePath)
	}
	if !foundInvariantDetailKey {
		t.Fatalf("%s DecodeProviderStream must include details key %q", filePath, "wire_stream_invariant")
	}
}

func assertDecodeStreamEntryDetailUsesInvariantError(t *testing.T, filePath string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", filePath, err)
	}

	var foundFunc bool
	var foundErrErrorCall bool
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "DecodeProviderStream" {
			return true
		}
		foundFunc = true
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "Error" {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if ok && x.Name == "err" {
				foundErrErrorCall = true
				return false
			}
			return true
		})
		return false
	})

	if !foundFunc {
		t.Fatalf("%s missing DecodeProviderStream", filePath)
	}
	if !foundErrErrorCall {
		t.Fatalf("%s DecodeProviderStream must include err.Error() for wire_stream_invariant detail", filePath)
	}
}
