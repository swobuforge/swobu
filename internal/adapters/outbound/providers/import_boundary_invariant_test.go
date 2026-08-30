package providers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderPackages_DoNotWrapDispatchedTransportFailuresAsBadEndpoints(t *testing.T) {
	t.Parallel()

	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isSelectorCall(call.Fun, "provider", "TransportFailure") {
				return true
			}
			for _, argument := range call.Args {
				ast.Inspect(argument, func(nested ast.Node) bool {
					nestedCall, ok := nested.(*ast.CallExpr)
					if ok && isSelectorCall(nestedCall.Fun, "canonical", "BadEndpoint") {
						t.Errorf("%s: dispatched transport failure contains canonical.BadEndpoint", fileSet.Position(nestedCall.Pos()))
						return false
					}
					return true
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk providers tree: %v", err)
	}
}

func isSelectorCall(expression ast.Expr, packageName, functionName string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != functionName {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == packageName
}

func TestProviderPackages_DoNotImportProtocolFamilyPackagesDirectly(t *testing.T) {
	t.Parallel()

	root := "."
	forbidden := []string{
		`"github.com/swobuforge/swobu/internal/wire/chatcompletions"`,
		`"github.com/swobuforge/swobu/internal/wire/messages"`,
		`"github.com/swobuforge/swobu/internal/wire/responses"`,
	}
	allowedPathSnippets := []string{
		"protocolcodec/",
		"/protocolcodec/",
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || filepath.Base(path) == "import_boundary_invariant_test.go" {
			return nil
		}
		for _, allowed := range allowedPathSnippets {
			if strings.Contains(path, allowed) {
				return nil
			}
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content := string(raw)
		for _, disallowed := range forbidden {
			if strings.Contains(content, disallowed) {
				t.Fatalf("forbidden direct protocol-family import in %s: %s", path, disallowed)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk providers tree: %v", err)
	}
}
