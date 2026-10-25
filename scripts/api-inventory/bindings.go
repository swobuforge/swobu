package main

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
)

// handlerBindings retains local definitions behind a registered handler name.
// It records every assignment rather than choosing one branch as authoritative.
// Only identifier aliases are followed: constructor arguments are dependencies,
// not additional handlers. Cross-package dispatch remains separate evidence.
func handlerBindings(function *ast.FuncDecl, handler ast.Expr, fileset *token.FileSet) map[string][]string {
	definitions := map[string][]ast.Expr{}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			// Constructors commonly return (handler, error). Retain the first
			// result binding; later tuple slots are not handler aliases.
			if len(node.Rhs) == 1 && len(node.Lhs) > 1 {
				if name, ok := node.Lhs[0].(*ast.Ident); ok {
					definitions[name.Name] = append(definitions[name.Name], node.Rhs[0])
				}
				return true
			}
			if len(node.Lhs) != len(node.Rhs) {
				return true
			}
			for i, left := range node.Lhs {
				if name, ok := left.(*ast.Ident); ok {
					definitions[name.Name] = append(definitions[name.Name], node.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			if len(node.Names) != len(node.Values) {
				return true
			}
			for i, name := range node.Names {
				definitions[name.Name] = append(definitions[name.Name], node.Values[i])
			}
		}
		return true
	})
	result := map[string][]string{}
	var visit func(ast.Expr)
	visit = func(expression ast.Expr) {
		name, ok := expression.(*ast.Ident)
		if !ok {
			return
		}
		if _, seen := result[name.Name]; seen {
			return
		}
		values := definitions[name.Name]
		if len(values) == 0 {
			return
		}
		result[name.Name] = []string{}
		for _, value := range values {
			var raw bytes.Buffer
			if printer.Fprint(&raw, fileset, value) != nil {
				continue
			}
			result[name.Name] = append(result[name.Name], raw.String())
			visit(value)
		}
	}
	visit(handler)
	if len(result) == 0 {
		return nil
	}
	return result
}
