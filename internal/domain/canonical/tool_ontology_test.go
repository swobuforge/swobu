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

func TestHistoricalScopedToolKeyPreservesExactClientIdentity(t *testing.T) {
	t.Parallel()

	key, err := HistoricalScopedToolKey("mcp__openaiDeveloperDocs", "search_openai_docs", ToolKindFunction)
	if err != nil {
		t.Fatal(err)
	}
	if key.Namespace() != "mcp__openaiDeveloperDocs" || key.Name() != "search_openai_docs" || key.Kind() != ToolKindFunction {
		t.Fatalf("historical scoped key = %#v", key)
	}
}

func TestHistoricalScopedToolKeyRejectsNormalizedIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		namespace string
		toolName  string
	}{
		{name: "namespace whitespace", namespace: " mcp__openaiDeveloperDocs", toolName: "search_openai_docs"},
		{name: "name whitespace", namespace: "mcp__openaiDeveloperDocs", toolName: "search_openai_docs "},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := HistoricalScopedToolKey(test.namespace, test.toolName, ToolKindFunction); err == nil {
				t.Fatal("HistoricalScopedToolKey accepted a normalized identity")
			}
		})
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
