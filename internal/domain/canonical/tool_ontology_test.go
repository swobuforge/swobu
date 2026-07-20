package canonical

import "testing"

func TestToolKeyIsTypedAndDeterministic(t *testing.T) {
	id := testRequestToolKey(ToolKindFunction, "lookup")
	if got, want := id.String(), "tool:v1/request/function/lookup"; got != want {
		t.Fatalf("id=%q want %q", got, want)
	}
	parsed, err := ParseToolKey(id.String())
	if err != nil || parsed != id {
		t.Fatal("tool key did not round trip")
	}
}

func TestToolDeclarationObjectValuesAreCanonicalAndCloned(t *testing.T) {
	schema := testToolSchema(`{"z":1,"a":{"b":2,"a":1}}`)
	decl := testFunctionTool(testRequestToolKey(ToolKindFunction, "lookup"), "", schema, Unspecified[bool]())
	function, ok := decl.Function()
	if !ok {
		t.Fatal("function branch missing")
	}
	if got, want := function.InputSchema().RawObject(), `{"a":{"a":1,"b":2},"z":1}`; got != want {
		t.Fatalf("schema=%s want %s", got, want)
	}
	clone, _ := decl.Clone().Function()
	if clone.Key() != decl.Key() || clone.InputSchema().RawObject() != function.InputSchema().RawObject() {
		t.Fatal("tool declaration clone lost semantics")
	}
}

func TestToolDeclarationKindsRemainClosed(t *testing.T) {
	object, _ := ParseJSONObject([]byte(`{"type":"grammar"}`))
	function := testFunctionTool(testRequestToolKey(ToolKindFunction, "f"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	custom := testCustomTool(testRequestToolKey(ToolKindCustom, "c"), "", NewToolFormatObject(object))
	if string(function.Kind()) != ToolTypeFunction || string(custom.Kind()) != ToolTypeCustom {
		t.Fatal("tool declaration kind projection changed")
	}
}
