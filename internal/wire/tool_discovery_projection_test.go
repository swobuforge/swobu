package wire

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestProjectToolDiscoveryPolyfillRetainsMaterializedDeclarations(t *testing.T) {
	loaded := mustProjectionFunction(t, "loaded")
	discovery := mustProjectionDiscovery(t, canonical.DiscoveryExecutorProvider)
	declarations, err := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, loaded})
	if err != nil {
		t.Fatal(err)
	}
	declarationItem, _ := canonical.NewToolDeclarationsItem(declarations, canonical.ContextScopeRequest)
	callID, _ := canonical.NewToolCallID("search_1")
	inputObject, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(inputObject), canonical.DiscoveryExecutorProvider)
	loadedSet, _ := canonical.NewToolSet([]canonical.ToolDeclaration{loaded})
	result, _ := canonical.NewToolDiscoveryResultItem(callID, loadedSet, canonical.DiscoveryExecutorProvider)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarationItem, call, result}})

	projection, err := ProjectToolDiscoveryPolyfill(request)
	if err != nil {
		t.Fatal(err)
	}
	items := projection.Request.Items()
	if len(items) != 1 {
		t.Fatalf("projected items=%d want 1", len(items))
	}
	contribution, ok := items[0].ToolDeclarations()
	if !ok {
		t.Fatal("projected item is not tool declarations")
	}
	got := contribution.Tools().Declarations()
	if len(got) != 1 || got[0].Key() != loaded.Key() {
		t.Fatalf("projected tools=%v want loaded tool", got)
	}
	if !projection.StructuralHistoryChange || len(projection.Changes) == 0 {
		t.Fatal("projection did not report structural compatibility change")
	}
}

func TestProjectToolDiscoveryPolyfillRejectsUnknownPendingInventory(t *testing.T) {
	discovery := mustProjectionDiscovery(t, canonical.DiscoveryExecutorClient)
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery})
	declarationItem, _ := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	callID, _ := canonical.NewToolCallID("search_1")
	inputObject, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(inputObject), canonical.DiscoveryExecutorClient)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarationItem, call}})

	if _, err := ProjectToolDiscoveryPolyfill(request); err == nil {
		t.Fatal("expected unknown pending inventory rejection")
	}
}

func TestProjectToolDiscoveryPolyfillRejectsBareLiveClientDiscovery(t *testing.T) {
	discovery := mustProjectionDiscovery(t, canonical.DiscoveryExecutorClient)
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery})
	declarationItem, _ := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarationItem}})

	assertProjectionIncompatible(t, request)
}

func TestProjectToolDiscoveryPolyfillRejectsLiveClientDiscoveryBesideUnrelatedFunction(t *testing.T) {
	discovery := mustProjectionDiscovery(t, canonical.DiscoveryExecutorClient)
	function := mustProjectionFunction(t, "unrelated")
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, function})
	declarationItem, _ := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarationItem}})

	assertProjectionIncompatible(t, request)
}

func TestProjectToolDiscoveryPolyfillRejectsLiveClientDeclarationAfterCompletedDiscovery(t *testing.T) {
	loaded := mustProjectionFunction(t, "loaded")
	discovery := mustProjectionDiscovery(t, canonical.DiscoveryExecutorClient)
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, loaded})
	declarationItem, _ := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	callID, _ := canonical.NewToolCallID("search_1")
	inputObject, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(inputObject), canonical.DiscoveryExecutorClient)
	loadedSet, _ := canonical.NewToolSet([]canonical.ToolDeclaration{loaded})
	result, _ := canonical.NewToolDiscoveryResultItem(callID, loadedSet, canonical.DiscoveryExecutorClient)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarationItem, call, result}})

	assertProjectionIncompatible(t, request)
}

func TestProjectToolDiscoveryPolyfillRejectsPendingDiscoveryBesideMaterializedFunction(t *testing.T) {
	discovery := mustProjectionDiscovery(t, canonical.DiscoveryExecutorProvider)
	function := mustProjectionFunction(t, "unrelated")
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, function})
	declarationItem, _ := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	callID, _ := canonical.NewToolCallID("search_pending")
	inputObject, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(inputObject), canonical.DiscoveryExecutorProvider)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarationItem, call}})

	assertProjectionIncompatible(t, request)
}

func TestProjectToolDiscoveryPolyfillPairsReusedCallIDByOccurrence(t *testing.T) {
	loadedA := mustProjectionFunction(t, "loaded_a")
	loadedB := mustProjectionFunction(t, "loaded_b")
	discovery := mustProjectionDiscovery(t, canonical.DiscoveryExecutorProvider)
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, loadedA, loadedB})
	declarationItem, _ := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	callID, _ := canonical.NewToolCallID("search_reused")
	inputObject, _ := canonical.ParseJSONObject([]byte(`{}`))
	firstCall, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(inputObject), canonical.DiscoveryExecutorProvider)
	firstSet, _ := canonical.NewToolSet([]canonical.ToolDeclaration{loadedA})
	firstResult, _ := canonical.NewToolDiscoveryResultItem(callID, firstSet, canonical.DiscoveryExecutorProvider)
	secondCall, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(inputObject), canonical.DiscoveryExecutorProvider)
	secondSet, _ := canonical.NewToolSet([]canonical.ToolDeclaration{loadedB})
	secondResult, _ := canonical.NewToolDiscoveryResultItem(callID, secondSet, canonical.DiscoveryExecutorProvider)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarationItem, firstCall, firstResult, secondCall, secondResult}})

	projection, err := ProjectToolDiscoveryPolyfill(request)
	if err != nil {
		t.Fatal(err)
	}
	items := projection.Request.Items()
	if len(items) != 1 {
		t.Fatalf("items=%d want one declaration contribution", len(items))
	}
}

func TestProjectToolDiscoveryPolyfillRejectsDuplicatePendingCallID(t *testing.T) {
	discovery := mustProjectionDiscovery(t, canonical.DiscoveryExecutorProvider)
	loaded := mustProjectionFunction(t, "loaded")
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, loaded})
	declarationItem, _ := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	callID, _ := canonical.NewToolCallID("search_duplicate")
	inputObject, _ := canonical.ParseJSONObject([]byte(`{}`))
	firstCall, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(inputObject), canonical.DiscoveryExecutorProvider)
	secondCall, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(inputObject), canonical.DiscoveryExecutorProvider)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarationItem, firstCall, secondCall}})

	if _, err := ProjectToolDiscoveryPolyfill(request); err == nil {
		t.Fatal("duplicate pending discovery call id was accepted")
	}
}

func TestProjectToolDiscoveryPolyfillIsNoOpForStaticRequest(t *testing.T) {
	function := mustProjectionFunction(t, "static")
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{function})
	declarationItem, _ := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarationItem}})

	projection, err := ProjectToolDiscoveryPolyfill(request)
	if err != nil {
		t.Fatal(err)
	}
	if projection.StructuralHistoryChange || len(projection.Changes) != 0 {
		t.Fatalf("static projection changed request: %+v", projection)
	}
}

func TestProjectToolDiscoveryPolyfillReportsDistinctSemanticLosses(t *testing.T) {
	loaded := mustProjectionFunction(t, "loaded")
	discovery := mustProjectionDiscovery(t, canonical.DiscoveryExecutorProvider)
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, loaded})
	visibility, _ := canonical.NewToolVisibilityRefinements(tools, []canonical.ToolKey{loaded.Key()})
	declarationItem, _ := canonical.NewToolDeclarationsItemWithVisibility(tools, canonical.ContextScopeHistory, visibility)
	callID, _ := canonical.NewToolCallID("search_1")
	inputObject, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(inputObject), canonical.DiscoveryExecutorProvider)
	loadedSet, _ := canonical.NewToolSet([]canonical.ToolDeclaration{loaded})
	result, _ := canonical.NewToolDiscoveryResultItem(callID, loadedSet, canonical.DiscoveryExecutorProvider)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarationItem, call, result}})

	projection, err := ProjectToolDiscoveryPolyfill(request)
	if err != nil {
		t.Fatal(err)
	}
	want := map[canonical.CapabilityPath]bool{
		canonical.RequestItemsKind:       false,
		canonical.RequestToolsKind:       false,
		canonical.RequestTools:           false,
		canonical.RequestToolsVisibility: false,
	}
	for _, change := range projection.Changes {
		if change.Kind == compat.Approximation {
			if _, ok := want[change.Capability]; ok {
				want[change.Capability] = true
			}
		}
	}
	for capability, found := range want {
		if !found {
			t.Fatalf("changes=%+v missing %s", projection.Changes, capability)
		}
	}
}

func mustProjectionFunction(t *testing.T, name string) canonical.ToolDeclaration {
	t.Helper()
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, name)
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	tool, err := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func mustProjectionDiscovery(t *testing.T, executor canonical.DiscoveryExecutor) canonical.ToolDeclaration {
	t.Helper()
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	tool, err := canonical.NewToolDiscoveryTool("find tools", canonical.NewToolSchemaObject(schemaObject), executor)
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func assertProjectionIncompatible(t *testing.T, request canonical.CanonicalRequest) {
	t.Helper()
	_, err := ProjectToolDiscoveryPolyfill(request)
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("error=%T %v want target incompatibility", err, err)
	}
}
