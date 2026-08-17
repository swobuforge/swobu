package canonical

import "testing"

func TestCompareReusablePrefixAppendChangeAndTruncation(t *testing.T) {
	a := prefixMessage(t, "a")
	b := prefixMessage(t, "b")
	c := prefixMessage(t, "c")
	d := prefixMessage(t, "d")
	request := func(items ...CanonicalItem) CanonicalRequest {
		return NewCanonicalRequest(RequestParams{Model: Specify("m"), Items: items})
	}
	tests := []struct {
		name       string
		previous   CanonicalRequest
		current    CanonicalRequest
		preserved  bool
		occurrence string
	}{
		{name: "append", previous: request(a, b, c), current: request(a, b, c, d), preserved: true},
		{name: "middle", previous: request(a, b, c), current: request(a, d, c), occurrence: "request:1"},
		{name: "truncate", previous: request(a, b, c), current: request(a, b), occurrence: "request:2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CompareReusablePrefix(test.previous, test.current)
			if got.Preserved != test.preserved {
				t.Fatalf("preserved = %t", got.Preserved)
			}
			if test.occurrence != "" && (got.InputChanged == nil || got.InputChanged.Key() != test.occurrence) {
				t.Fatalf("input occurrence = %#v", got.InputChanged)
			}
		})
	}
}

func TestCompareReusablePrefixPreservesSemanticOrderAndCanonicalObjects(t *testing.T) {
	firstObject, _ := ParseJSONObject([]byte(`{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"number"}}}`))
	secondObject, _ := ParseJSONObject([]byte(`{"properties":{"b":{"type":"number"},"a":{"type":"string"}},"type":"object"}`))
	firstSchema := NewToolSchemaObject(firstObject)
	secondSchema := NewToolSchemaObject(secondObject)
	keyA, _ := NewRequestToolKey(ToolKindFunction, "a")
	keyB, _ := NewRequestToolKey(ToolKindFunction, "b")
	toolA1, _ := NewFunctionTool(keyA, "a", firstSchema, Unspecified[bool]())
	toolA2, _ := NewFunctionTool(keyA, "a", secondSchema, Unspecified[bool]())
	toolB, _ := NewFunctionTool(keyB, "b", firstSchema, Unspecified[bool]())
	request := func(tools ...ToolDeclaration) CanonicalRequest {
		set, _ := NewToolSet(tools)
		declarations, _ := NewToolDeclarationsItem(set, ContextScopeRequest)
		return NewCanonicalRequest(RequestParams{Model: Specify("m"), Items: []CanonicalItem{declarations, prefixMessage(t, "hello")}})
	}
	if got := CompareReusablePrefix(request(toolA1, toolB), request(toolA2, toolB)); !got.Preserved {
		t.Fatalf("object key order changed prefix: %#v", got)
	}
	if got := CompareReusablePrefix(request(toolA1, toolB), request(toolB, toolA1)); got.ToolChanged == nil || got.ToolChanged.Key() != "tool-index:0" {
		t.Fatalf("tool order comparison = %#v", got)
	}
}

func TestCompareReusablePrefixReportsInstructionOccurrence(t *testing.T) {
	instruction := func(text string) CanonicalItem {
		item, _ := NewScopedMessageItem(MessageRoleDeveloper, []MessagePart{NewTextMessagePart(text)}, ContextScopeRequest)
		return item
	}
	previous := NewCanonicalRequest(RequestParams{Model: Specify("m"), Items: []CanonicalItem{instruction("a"), prefixMessage(t, "hello")}})
	current := NewCanonicalRequest(RequestParams{Model: Specify("m"), Items: []CanonicalItem{instruction("b"), prefixMessage(t, "hello")}})
	got := CompareReusablePrefix(previous, current)
	if got.InstructionChanged == nil || got.InstructionChanged.Key() != "request:0" {
		t.Fatalf("comparison = %#v", got)
	}
}

func TestCompareReusablePrefixTreatsEarlierBandInsertionAsChange(t *testing.T) {
	instruction := func(text string) CanonicalItem {
		item, _ := NewScopedMessageItem(MessageRoleDeveloper, []MessagePart{NewTextMessagePart(text)}, ContextScopeRequest)
		return item
	}
	tool := func(name string) CanonicalItem {
		key, _ := NewRequestToolKey(ToolKindFunction, name)
		declaration, _ := NewFunctionTool(key, name, NewToolSchemaObject(EmptyJSONObject()), Unspecified[bool]())
		set, _ := NewToolSet([]ToolDeclaration{declaration})
		item, _ := NewToolDeclarationsItem(set, ContextScopeRequest)
		return item
	}
	input := prefixMessage(t, "hello")
	tests := []struct {
		name       string
		current    CanonicalRequest
		changeKind string
		occurrence string
	}{
		{
			name:       "instruction before existing input",
			current:    NewCanonicalRequest(RequestParams{Model: Specify("m"), Items: []CanonicalItem{instruction("new"), input}}),
			changeKind: "instruction",
			occurrence: "request:0",
		},
		{
			name:       "tool before existing input",
			current:    NewCanonicalRequest(RequestParams{Model: Specify("m"), Items: []CanonicalItem{tool("lookup"), input}}),
			changeKind: "tool",
			occurrence: "tool-index:0",
		},
	}
	previous := NewCanonicalRequest(RequestParams{Model: Specify("m"), Items: []CanonicalItem{input}})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CompareReusablePrefix(previous, test.current)
			var occurrence *Occurrence
			switch test.changeKind {
			case "instruction":
				occurrence = got.InstructionChanged
			case "tool":
				occurrence = got.ToolChanged
			}
			if occurrence == nil || occurrence.Key() != test.occurrence {
				t.Fatalf("comparison = %#v", got)
			}
		})
	}
}

func TestCompareReusablePrefixIncludesToolOccurrenceVisibility(t *testing.T) {
	schema := NewToolSchemaObject(EmptyJSONObject())
	key, _ := NewRequestToolKey(ToolKindFunction, "lookup")
	tool, _ := NewFunctionTool(key, "lookup", schema, Unspecified[bool]())
	set, _ := NewToolSet([]ToolDeclaration{tool})
	visible, _ := NewToolDeclarationsItem(set, ContextScopeRequest)
	deferred, _ := NewToolVisibilityRefinements(set, []ToolKey{key})
	hidden, _ := NewToolDeclarationsItemWithVisibility(set, ContextScopeRequest, deferred)
	previous := NewCanonicalRequest(RequestParams{Model: Specify("m"), Items: []CanonicalItem{visible, prefixMessage(t, "hi")}})
	current := NewCanonicalRequest(RequestParams{Model: Specify("m"), Items: []CanonicalItem{hidden, prefixMessage(t, "hi")}})
	got := CompareReusablePrefix(previous, current)
	if got.ToolChanged == nil || got.ToolChanged.Key() != "tool-index:0" {
		t.Fatalf("visibility comparison = %#v", got)
	}
}

func prefixMessage(t *testing.T, text string) CanonicalItem {
	t.Helper()
	item, err := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart(text)})
	if err != nil {
		t.Fatal(err)
	}
	return item
}
