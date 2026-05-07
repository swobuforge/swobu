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

func TestCockpitViews_NoUseStateInsideIfOrSwitch(t *testing.T) {
	t.Parallel()

	root := cockpitViewsRoot(t)
	var violations []string
	fset := token.NewFileSet()
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
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		violations = append(violations, findConditionalUseStateViolations(fset, path, file)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk cockpit views: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("forbidden conditional UseState calls found:\n%s", strings.Join(violations, "\n"))
	}
}

func TestFindConditionalUseStateViolations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		src       string
		wantCount int
	}{
		{
			name: "top-level UseState allowed",
			src: `package p
func f(ctx any) {
	_, _ = view.UseState(ctx, func() bool { return false })
}`,
			wantCount: 0,
		},
		{
			name: "if branch forbidden",
			src: `package p
func f(ctx any, ok bool) {
	if ok {
		_, _ = view.UseState(ctx, func() bool { return false })
	}
}`,
			wantCount: 1,
		},
		{
			name: "switch case forbidden",
			src: `package p
func f(ctx any, s string) {
	switch s {
	case "x":
		_, _ = view.UseState(ctx, func() bool { return false })
	}
}`,
			wantCount: 1,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "snippet.go", tt.src, 0)
			if err != nil {
				t.Fatalf("parse snippet: %v", err)
			}
			got := findConditionalUseStateViolations(fset, "snippet.go", file)
			if len(got) != tt.wantCount {
				t.Fatalf("violations=%d want=%d\n%v", len(got), tt.wantCount, got)
			}
		})
	}
}

func findConditionalUseStateViolations(fset *token.FileSet, path string, file *ast.File) []string {
	var violations []string
	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isUseStateCall(call) {
			return true
		}
		if !hasConditionalAncestor(stack[:len(stack)-1]) {
			return true
		}
		pos := fset.Position(call.Pos())
		violations = append(violations, pos.String()+": UseState inside if/switch is forbidden")
		return true
	})
	return violations
}

func isUseStateCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel != nil && sel.Sel.Name == "UseState"
}

func hasConditionalAncestor(ancestors []ast.Node) bool {
	for _, n := range ancestors {
		switch n.(type) {
		case *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt:
			return true
		}
	}
	return false
}

func cockpitViewsRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// .../internal/terminalui/apps/cockpit/app/views/lint -> .../views
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), ".."))
}
