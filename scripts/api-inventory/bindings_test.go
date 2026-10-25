package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestHandlerBindingsRetainAliasesBranchesAndCycles(t *testing.T) {
	fileset := token.NewFileSet()
	file, err := parser.ParseFile(fileset, "mux.go", `package bootstrap
 func mount(){
  base, err := NewControlHandler(service)
  handler := base
  if enabled { handler = NewDataHandler(service) }
  base = handler
  mux.Handle("/api/v1/workspaces/",handler)
 }`, 0)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*ast.FuncDecl)
	bindings := handlerBindings(function, ast.NewIdent("handler"), fileset)
	if len(bindings) != 2 || len(bindings["handler"]) != 2 || len(bindings["base"]) != 2 {
		t.Fatalf("bindings=%v", bindings)
	}
	if bindings["base"][0] != "NewControlHandler(service)" || bindings["handler"][1] != "NewDataHandler(service)" {
		t.Fatalf("bindings=%v", bindings)
	}
	if bindings := handlerBindings(function, ast.NewIdent("external"), fileset); bindings != nil {
		t.Fatalf("invented external binding=%v", bindings)
	}
}
