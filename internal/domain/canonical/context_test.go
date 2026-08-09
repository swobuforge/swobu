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

func TestTransformToolContributionsPreservesEveryCarrierMetadata(t *testing.T) {
	first := testFunctionTool(testRequestToolKey(ToolKindFunction, "first"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	replacement := testFunctionTool(testRequestToolKey(ToolKindFunction, "replacement"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	firstSet, _ := NewToolSet([]ToolDeclaration{first})
	replacementSet, _ := NewToolSet([]ToolDeclaration{replacement})
	declarations, _ := NewToolDeclarationsItem(firstSet, ContextScopeRequest)
	callID, _ := NewToolCallID("discovery_1")
	discovery, _ := NewToolDiscoveryResultItem(callID, firstSet, DiscoveryExecutorProvider)
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{declarations, discovery}})

	rewritten, err := TransformToolContributions(request, func(ToolSet) (ToolSet, error) {
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

func TestTransformToolContributionsRetainsResponsesRefinementsByExactCallableKey(t *testing.T) {
	removedKey, _ := NewToolKey("request/workspace", ToolKindFunction, "removed")
	survivingKey := testRequestToolKey(ToolKindFunction, "surviving")
	plainKey, _ := NewToolKey("request/workspace", ToolKindFunction, "plain")
	addedKey := testRequestToolKey(ToolKindCustom, "added")
	removed := testFunctionTool(removedKey, "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	surviving := testFunctionTool(survivingKey, "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	plain := testFunctionTool(plainKey, "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	formatObject, err := ParseJSONObject([]byte(`{"type":"text"}`))
	if err != nil {
		t.Fatal(err)
	}
	added, err := NewCustomTool(addedKey, "", NewToolFormatObject(formatObject))
	if err != nil {
		t.Fatal(err)
	}
	namespaceKey := testRequestToolKey(ToolKindNamespace, "workspace")
	namespace, err := NewToolNamespace(namespaceKey, "", []ToolDeclaration{removed, plain})
	if err != nil {
		t.Fatal(err)
	}
	before, err := NewToolSet([]ToolDeclaration{namespace, surviving})
	if err != nil {
		t.Fatal(err)
	}
	refinements, err := NewToolVisibilityRefinements(before, []ToolKey{removedKey, survivingKey})
	if err != nil {
		t.Fatal(err)
	}
	declarations, err := NewToolDeclarationsItemWithVisibility(before, ContextScopeRequest, refinements)
	if err != nil {
		t.Fatal(err)
	}
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{declarations}})
	afterNamespace, err := NewToolNamespace(namespaceKey, "", []ToolDeclaration{plain})
	if err != nil {
		t.Fatal(err)
	}
	after, err := NewToolSet([]ToolDeclaration{surviving, added, afterNamespace})
	if err != nil {
		t.Fatal(err)
	}

	transformed, err := TransformToolContributions(request, func(ToolSet) (ToolSet, error) {
		return after, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	occurrence, ok := transformed.Items()[0].ToolDeclarations()
	if !ok {
		t.Fatalf("transformed item = %#v, want tool declarations", transformed.Items()[0])
	}
	got := occurrence.Visibility()
	if !got.Deferred(survivingKey) {
		t.Fatal("surviving exact key lost its Responses refinement")
	}
	for _, key := range []ToolKey{removedKey, plainKey, addedKey} {
		if got.Deferred(key) {
			t.Fatalf("Responses refinement leaked to %q", key)
		}
	}
	if keys := got.DeferredKeys(); len(keys) != 1 {
		t.Fatalf("retained deferred keys = %#v, want only %q", keys, survivingKey)
	}
}

func TestTransformToolContributionsPreservesDiscoveryFailure(t *testing.T) {
	callID, _ := NewToolCallID("search_failed")
	failure, err := NewToolDiscoveryFailureItem(callID, DiscoveryExecutorProvider, Specify("unavailable"), "search offline")
	if err != nil {
		t.Fatal(err)
	}
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{failure}})
	functionKey, _ := NewRequestToolKey(ToolKindFunction, "unexpected")
	schemaObject, _ := ParseJSONObject([]byte(`{"type":"object"}`))
	function, _ := NewFunctionTool(functionKey, "", NewToolSchemaObject(schemaObject), Unspecified[bool]())
	functionSet, _ := NewToolSet([]ToolDeclaration{function})

	transformed, err := TransformToolContributions(request, func(ToolSet) (ToolSet, error) {
		return functionSet, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := transformed.Items()[0].ToolDiscoveryResult()
	if !ok {
		t.Fatal("transformed item is not discovery result")
	}
	got, ok := result.Failure()
	code, _ := got.Code().Get()
	if !ok || code != "unavailable" || got.Message() != "search offline" || !result.Tools().IsEmpty() {
		t.Fatalf("failure=(%+v,%t) tools=%v", got, ok, result.Tools().Declarations())
	}
}
