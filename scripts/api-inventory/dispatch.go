package main

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"sort"
	"strings"
)

// dispatchCondition retains the complete condition rather than inferring that
// every referenced method/path combination is admitted. For example, a negative
// method check is rejection evidence, not a mounted operation declaration.
type dispatchCondition struct {
	Source    string   `json:"source"`
	Function  string   `json:"function"`
	Kind      string   `json:"kind"`
	Condition string   `json:"condition"`
	Methods   []string `json:"methods"`
	Context   []string `json:"context,omitempty"`
}

func handlerDispatch(function *ast.FuncDecl, source string, fileset *token.FileSet) []dispatchCondition {
	if !strings.Contains(source, "inbound/httpapi/") {
		return nil
	}
	var result []dispatchCondition
	var ancestors []ast.Node
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if node == nil {
			ancestors = ancestors[:len(ancestors)-1]
			return true
		}
		context := dispatchContext(ancestors, node, fileset)
		ancestors = append(ancestors, node)
		var expressions []ast.Expr
		kind := ""
		switch node := node.(type) {
		case *ast.IfStmt:
			expressions = []ast.Expr{node.Cond}
			kind = "if"
		case *ast.SwitchStmt:
			if node.Tag != nil {
				expressions = []ast.Expr{node.Tag}
				kind = "switch"
			}
		case *ast.CaseClause:
			expressions = node.List
			kind = "case"
		default:
			return true
		}
		for _, expression := range expressions {
			var raw bytes.Buffer
			if printer.Fprint(&raw, fileset, expression) != nil {
				return true
			}
			text := raw.String()
			if !strings.Contains(text, ".Method") && !strings.Contains(text, ".URL.Path") && !strings.Contains(text, "http.Method") {
				continue
			}
			methods := map[string]bool{}
			ast.Inspect(expression, func(child ast.Node) bool {
				selector, ok := child.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := selector.X.(*ast.Ident)
				if ok && pkg.Name == "http" && strings.HasPrefix(selector.Sel.Name, "Method") {
					methods[strings.TrimPrefix(selector.Sel.Name, "Method")] = true
				}
				return true
			})
			names := []string{}
			for name := range methods {
				names = append(names, name)
			}
			sort.Strings(names)
			result = append(result, dispatchCondition{source, function.Name.Name, kind, text, names, context})
		}
		return true
	})
	return result
}

// Context preserves lexical branch membership; it does not infer that earlier
// guards returned or that a referenced method/path combination is admitted.
func dispatchContext(ancestors []ast.Node, child ast.Node, fileset *token.FileSet) []string {
	var result []string
	render := func(expression ast.Expr) string {
		var raw bytes.Buffer
		_ = printer.Fprint(&raw, fileset, expression)
		return raw.String()
	}
	for _, ancestor := range ancestors {
		switch parent := ancestor.(type) {
		case *ast.IfStmt:
			branch := "then "
			if parent.Else != nil && child.Pos() >= parent.Else.Pos() {
				branch = "else "
			}
			result = append(result, branch+"("+render(parent.Cond)+")")
		case *ast.SwitchStmt:
			if parent.Tag != nil {
				result = append(result, "switch "+render(parent.Tag))
			} else {
				result = append(result, "switch true")
			}
		case *ast.CaseClause:
			labels := []string{}
			for _, expression := range parent.List {
				labels = append(labels, render(expression))
			}
			if len(labels) == 0 {
				result = append(result, "default")
			} else {
				result = append(result, "case "+strings.Join(labels, ", "))
			}
		}
	}
	return result
}
