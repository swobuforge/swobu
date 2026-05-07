package lint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestViewCode_NoRuneLenMathOutsideMetricsBoundary(t *testing.T) {
	t.Parallel()

	roots := []string{
		cockpitViewsRoot(t),
		toolkitViewsRoot(t),
		cliViewsRoot(t),
	}
	fset := token.NewFileSet()
	var violations []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if strings.HasSuffix(path, "text_fill.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			violations = append(violations, findRuneMathViolations(fset, path, file)...)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("forbidden rune-length char math in view code:\n%s", strings.Join(violations, "\n"))
	}
}

func findRuneMathViolations(fset *token.FileSet, path string, file *ast.File) []string {
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "len" {
			return true
		}
		_, isRuneCast := call.Args[0].(*ast.CallExpr)
		if !isRuneCast {
			return true
		}
		render := renderNodeExpr(call.Args[0])
		if !strings.HasPrefix(render, "[]rune(") {
			return true
		}
		pos := fset.Position(call.Pos())
		out = append(out, pos.String()+": forbidden len([]rune(...)); use toolkit RuneLen/TextMetrics")
		return true
	})
	return out
}

func renderNodeExpr(n ast.Node) string {
	var b strings.Builder
	_ = formatNode(&b, n)
	return b.String()
}

func formatNode(b *strings.Builder, n ast.Node) error {
	switch x := n.(type) {
	case *ast.CallExpr:
		formatNode(b, x.Fun)
		b.WriteByte('(')
		for i, arg := range x.Args {
			if i > 0 {
				b.WriteString(",")
			}
			formatNode(b, arg)
		}
		b.WriteByte(')')
	case *ast.ArrayType:
		b.WriteString("[]")
		formatNode(b, x.Elt)
	case *ast.Ident:
		b.WriteString(x.Name)
	default:
		return nil
	}
	return nil
}

func toolkitViewsRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../../../toolkit/views"))
}

func cliViewsRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../../../apps/cli/app/views"))
}

