package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestManualDispatchConditionsRetainRejectionAndSwitchSemantics(t *testing.T) {
	source := `package httpapi
func handle(){
 if r.Method != http.MethodPost || r.URL.Path != "/api/v1/example" { return }
 switch r.Method {case http.MethodGet: read();case http.MethodPut:write()}
 switch {case r.Method==http.MethodGet && strings.HasSuffix(path,"/inspect"):inspect()}
}`
	fileset := token.NewFileSet()
	file, err := parser.ParseFile(fileset, "handler.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	conditions := handlerDispatch(file.Decls[0].(*ast.FuncDecl), "internal/adapters/inbound/httpapi/handler.go", fileset)
	if len(conditions) != 5 {
		t.Fatalf("conditions=%+v", conditions)
	}
	foundNegative, foundInspect := false, false
	for _, condition := range conditions {
		if strings.Contains(condition.Condition, "!= http.MethodPost") {
			foundNegative = condition.Kind == "if" && len(condition.Methods) == 1 && condition.Methods[0] == "Post"
		}
		if strings.Contains(condition.Condition, "/inspect") {
			foundInspect = len(condition.Methods) == 1 && condition.Methods[0] == "Get"
		}
	}
	if !foundNegative || !foundInspect {
		t.Fatal("lost manual dispatch semantics")
	}
}

func TestDispatchInventoryDetectsMethodMovedBetweenPathBranches(t *testing.T) {
	capture := func(methodInFirst bool) string {
		t.Helper()
		first, second := "read()", "if r.Method == http.MethodPost { write() }"
		if methodInFirst {
			first, second = second, first
		}
		source := `package httpapi
func handle(){ if path == "/one" { ` + first + ` } else { ` + second + ` } }`
		fileset := token.NewFileSet()
		file, err := parser.ParseFile(fileset, "handler.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		conditions := handlerDispatch(file.Decls[0].(*ast.FuncDecl), "internal/adapters/inbound/httpapi/handler.go", fileset)
		raw, err := json.Marshal(conditions)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if capture(true) == capture(false) {
		t.Fatal("moving method dispatch between path branches leaves inventory unchanged")
	}
}

func TestDispatchContextRetainsElseAndSwitchCases(t *testing.T) {
	fileset := token.NewFileSet()
	file, err := parser.ParseFile(fileset, "handler.go", `package httpapi
 func handle(){
  if path=="/one" { read() } else {
   switch r.Method {
   case http.MethodPost:
    if allowed { write() }
   default:
    if r.Method!=http.MethodGet { reject() }
   }
  }
 }`, 0)
	if err != nil {
		t.Fatal(err)
	}
	conditions := handlerDispatch(file.Decls[0].(*ast.FuncDecl), "internal/adapters/inbound/httpapi/handler.go", fileset)
	foundCase, foundDefault := false, false
	for _, condition := range conditions {
		context := strings.Join(condition.Context, " | ")
		if condition.Kind == "case" && condition.Condition == "http.MethodPost" {
			foundCase = strings.Contains(context, `else (path == "/one")`) && strings.Contains(context, "switch r.Method")
		}
		if condition.Kind == "if" && strings.Contains(condition.Condition, "!= http.MethodGet") {
			foundDefault = strings.Contains(context, "default") && strings.Contains(context, `else (path == "/one")`)
		}
	}
	if !foundCase || !foundDefault {
		t.Fatalf("missing branch context: %+v", conditions)
	}
}
