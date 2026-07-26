package canonical

import "testing"

func TestContextOperationsPreserveScopeAndFoldEnvironment(t *testing.T) {
	requestDirective, _ := NewScopedMessageItem(MessageRoleDeveloper, []MessagePart{NewTextMessagePart("base")}, ContextScopeRequest)
	historyDirective, _ := NewScopedMessageItem(MessageRoleSystem, []MessagePart{NewTextMessagePart("prior")}, ContextScopeHistory)
	first := testFunctionTool(testRequestToolKey(ToolKindFunction, "first"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	second := testFunctionTool(testRequestToolKey(ToolKindFunction, "second"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	firstSet, _ := NewToolSet([]ToolDeclaration{first})
	secondSet, _ := NewToolSet([]ToolDeclaration{second})
	requestDeclarations, _ := NewToolDeclarationsItem(firstSet, ContextScopeRequest)
	historyDeclarations, _ := NewToolDeclarationsItem(secondSet, ContextScopeHistory)
	items := []CanonicalItem{requestDirective, requestDeclarations, historyDirective, historyDeclarations}

	prelude, rest, err := SplitRequestPrelude(items)
	if err != nil || len(prelude.Items()) != 2 || len(rest) != 2 {
		t.Fatalf("split = %d/%d, err=%v", len(prelude.Items()), len(rest), err)
	}
	if retained := RetainedHistory(items); len(retained) != 2 {
		t.Fatalf("retained history = %d, want 2", len(retained))
	}
	environment, err := ToolEnvironmentAt(items, len(items))
	if err != nil || len(environment.Declarations()) != 2 {
		t.Fatalf("environment = %#v, err=%v", environment.Declarations(), err)
	}
}

func TestToolEnvironmentRejectsConflictingRedeclaration(t *testing.T) {
	key := testRequestToolKey(ToolKindFunction, "lookup")
	left := testFunctionTool(key, "left", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	right := testFunctionTool(key, "right", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	leftSet, _ := NewToolSet([]ToolDeclaration{left})
	rightSet, _ := NewToolSet([]ToolDeclaration{right})
	leftItem, _ := NewToolDeclarationsItem(leftSet, ContextScopeHistory)
	rightItem, _ := NewToolDeclarationsItem(rightSet, ContextScopeHistory)
	if _, err := ToolEnvironmentAt([]CanonicalItem{leftItem, rightItem}, 2); err == nil {
		t.Fatal("conflicting redeclaration must be ambiguous")
	}
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{leftItem, rightItem}})
	if _, err := request.EffectiveToolPolicy(); err == nil {
		t.Fatal("effective tool policy swallowed an ambiguous environment")
	}
}

func TestToolEnvironmentRejectsEquivalentDeclarationWithContradictoryOwner(t *testing.T) {
	childKey, _ := NewToolKey("request/group", ToolKindFunction, "lookup")
	child := testFunctionTool(
		childKey, "", testToolSchema(`{"type":"object"}`), Unspecified[bool](),
	)
	parentKey := testRequestToolKey(ToolKindNamespace, "group")
	parent, err := NewToolNamespace(parentKey, "", []ToolDeclaration{child})
	if err != nil {
		t.Fatal(err)
	}
	parentSet, _ := NewToolSet([]ToolDeclaration{parent})
	childSet, _ := NewToolSet([]ToolDeclaration{child})
	parentItem, _ := NewToolDeclarationsItem(parentSet, ContextScopeHistory)
	childItem, _ := NewToolDeclarationsItem(childSet, ContextScopeHistory)
	if _, err := ToolEnvironmentAt(
		[]CanonicalItem{parentItem, childItem}, 2,
	); err == nil {
		t.Fatal("equivalent declaration with contradictory owner was admitted")
	}
}

func TestRewriteToolContributionsPreservesEveryCarrierMetadata(t *testing.T) {
	first := testFunctionTool(testRequestToolKey(ToolKindFunction, "first"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	replacement := testFunctionTool(testRequestToolKey(ToolKindFunction, "replacement"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	firstSet, _ := NewToolSet([]ToolDeclaration{first})
	replacementSet, _ := NewToolSet([]ToolDeclaration{replacement})
	declarations, _ := NewToolDeclarationsItem(firstSet, ContextScopeRequest)
	callID, _ := NewToolCallID("discovery_1")
	discovery, _ := NewToolDiscoveryResultItem(callID, firstSet, DiscoveryExecutorProvider)
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{declarations, discovery}})

	rewritten, err := RewriteToolContributions(request, func(ToolSet) (ToolSet, error) {
		return replacementSet, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	items := rewritten.Items()
	occurrence, ok := items[0].ToolDeclarations()
	if !ok || occurrence.Scope() != ContextScopeRequest || occurrence.Tools().Declarations()[0].Key() != replacement.Key() {
		t.Fatalf("declaration occurrence = %#v", items[0])
	}
	result, ok := items[1].ToolDiscoveryResult()
	if !ok || result.CallID() != callID || result.Executor() != DiscoveryExecutorProvider ||
		result.Tools().Declarations()[0].Key() != replacement.Key() {
		t.Fatalf("discovery contribution = %#v", items[1])
	}
}
