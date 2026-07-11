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
		{name: "chatcompletions", rel: filepath.Join("..", "adapters", "wire", "families", "chatcompletions", "provider_codec.go")},
		{name: "completions", rel: filepath.Join("..", "adapters", "wire", "families", "completions", "provider_codec.go")},
		{name: "messages", rel: filepath.Join("..", "adapters", "wire", "families", "messages", "provider_codec.go")},
		{name: "responses", rel: filepath.Join("..", "adapters", "wire", "families", "responses", "provider_codec.go")},
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
		if !ok || fn.Name == nil || fn.Name.Name != "DecodeProviderEnvelope" {
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
		t.Fatalf("%s missing DecodeProviderEnvelope", filePath)
	}
	if !foundValidateCall {
		t.Fatalf("%s DecodeProviderEnvelope must call core.ValidateResponseSSECarrierStream", filePath)
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
		if !ok || fn.Name == nil || fn.Name.Name != "DecodeProviderEnvelope" {
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
		t.Fatalf("%s missing DecodeProviderEnvelope", filePath)
	}
	if !foundInvariantDetailKey {
		t.Fatalf("%s DecodeProviderEnvelope must include details key %q", filePath, "wire_stream_invariant")
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
		if !ok || fn.Name == nil || fn.Name.Name != "DecodeProviderEnvelope" {
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
		t.Fatalf("%s missing DecodeProviderEnvelope", filePath)
	}
	if !foundErrErrorCall {
		t.Fatalf("%s DecodeProviderEnvelope must include err.Error() for wire_stream_invariant detail", filePath)
	}
}
