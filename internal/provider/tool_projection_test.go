package provider

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire/toolname"
)

func TestAttemptToolProjectionIsBoundedTotalAndCollisionSafe(t *testing.T) {
	first, _ := canonical.NewToolKey("mcp/"+strings.Repeat("very-long-namespace/", 8), canonical.ToolKindFunction, strings.Repeat("create_issue", 8))
	second, _ := canonical.NewToolKey("mcp/a-b", canonical.ToolKindFunction, "same")
	third, _ := canonical.NewToolKey("mcp/a_b", canonical.ToolKindFunction, "same")
	plain, _ := canonical.NewToolKey(canonical.ToolNamespaceRequest, canonical.ToolKindFunction, "lookup")
	request := projectionTestRequest(t, first, second, third, plain)

	projected, table, _, err := ProjectAttemptTools(request)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, declaration := range canonicaltest.Tools(projected) {
		name := declaration.Key().Name()
		if len(name) > toolname.MaxLength || !toolname.Safe(name) {
			t.Fatalf("unsafe projection %q", name)
		}
		if seen[name] {
			t.Fatalf("projection collision %q", name)
		}
		seen[name] = true
		original, ok := table.OriginalKey(declaration.Key())
		if !ok || original != canonicaltest.Tools(request)[len(seen)-1].Key() {
			t.Fatalf("projection did not round-trip: %#v", declaration.Key())
		}
	}
	if wire, _ := table.WireName(plain); wire != "lookup" {
		t.Fatalf("safe request name = %q, want preserved", wire)
	}
}

func TestOneProjectionRewritesAttemptDecodeContextAndReverseLookup(t *testing.T) {
	key, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, strings.Repeat("search", 20))
	full := projectionTestRequest(t, key)
	callID, _ := canonical.NewToolCallID("call_1")
	input, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, _ := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(input))
	attempt := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{call},
	})
	projection, err := BuildToolProjection(full)
	if err != nil {
		t.Fatal(err)
	}
	projectedFull, _, err := projection.Rewrite(full)
	if err != nil {
		t.Fatal(err)
	}
	projectedAttempt, _, err := projection.Rewrite(attempt)
	if err != nil {
		t.Fatal(err)
	}
	declarationWire := canonicaltest.Tools(projectedFull)[0].Key()
	projectedCall, _ := projectedAttempt.Items()[0].ToolCall()
	if projectedCall.Tool() != declarationWire {
		t.Fatalf("attempt key %q differs from decode context %q", projectedCall.Tool(), declarationWire)
	}
	if original, ok := projection.Table().OriginalKey(projectedCall.Tool()); !ok || original != key {
		t.Fatalf("reverse lookup = %q, %t", original, ok)
	}
}

func projectionTestRequest(t *testing.T, keys ...canonical.ToolKey) canonical.CanonicalRequest {
	t.Helper()
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	declarations := make([]canonical.ToolDeclaration, len(keys))
	for i, key := range keys {
		declarations[i] = canonicaltest.MustFunctionTool(key, "", canonical.NewToolSchemaObject(object), canonical.Unspecified[bool]())
	}
	set, err := canonical.NewToolSet(declarations)
	if err != nil {
		t.Fatal(err)
	}
	declarationItem, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	return canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{declarationItem}})
}

func TestLiteralReservedLookingWireNameStaysRequestScoped(t *testing.T) {
	key, err := canonical.ToolIdentityFromWire("swobu__looks__projected", canonical.ToolKindFunction)
	if err != nil || key.Namespace() != canonical.ToolNamespaceRequest || key.Name() != "swobu__looks__projected" {
		t.Fatalf("literal identity was reinterpreted: %#v err=%v", key, err)
	}
}

func TestAttemptToolProjectionAvoidsLiteralAliasCollision(t *testing.T) {
	namespaced, _ := canonical.NewToolKey("mcp/example", canonical.ToolKindFunction, "lookup")
	firstAlias := toolname.Alias(namespaced.String(), namespaced.Name(), 0)
	literal, _ := canonical.NewToolKey(canonical.ToolNamespaceRequest, canonical.ToolKindFunction, firstAlias)
	request := projectionTestRequest(t, namespaced, literal)
	projected, table, _, err := ProjectAttemptTools(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonicaltest.Tools(projected)) != 2 || canonicaltest.Tools(projected)[0].Key().Name() == canonicaltest.Tools(projected)[1].Key().Name() {
		t.Fatal("projection allocator did not avoid a reserved literal name")
	}
	for _, declaration := range canonicaltest.Tools(projected) {
		if _, ok := table.OriginalKey(declaration.Key()); !ok {
			t.Fatalf("projection lost reverse mapping for %q", declaration.Key().Name())
		}
	}
}

func TestAttemptToolProjectionAllocatesCollisionSafeNamespaceChildren(t *testing.T) {
	leftKey, _ := canonical.NewToolKey("mcp/left", canonical.ToolKindFunction, "same")
	rightKey, _ := canonical.NewToolKey("mcp/right", canonical.ToolKindFunction, "same")
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	left := canonicaltest.MustFunctionTool(leftKey, "", canonical.NewToolSchemaObject(object), canonical.Unspecified[bool]())
	right := canonicaltest.MustFunctionTool(rightKey, "", canonical.NewToolSchemaObject(object), canonical.Unspecified[bool]())
	namespaceKey, _ := canonical.NewRequestToolKey(canonical.ToolKindNamespace, "remote")
	namespace, err := canonical.NewToolNamespace(namespaceKey, "", []canonical.ToolDeclaration{left, right})
	if err != nil {
		t.Fatal(err)
	}
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{namespace})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}})

	projected, table, _, err := ProjectAttemptTools(request)
	if err != nil {
		t.Fatal(err)
	}
	tools := canonicaltest.Tools(projected)
	projectedNamespace, ok := tools[0].Namespace()
	if !ok || len(projectedNamespace.Tools()) != 2 {
		t.Fatalf("projected namespace = %#v", tools)
	}
	children := projectedNamespace.Tools()
	if children[0].Key().Name() == children[1].Key().Name() {
		t.Fatalf("namespace child aliases collided: %#v", children)
	}
	for _, original := range []canonical.ToolKey{leftKey, rightKey} {
		wireName, err := table.WireName(original)
		if err != nil {
			t.Fatal(err)
		}
		key, ok := table.CanonicalKey(canonical.ToolKindFunction, wireName)
		if !ok || key != original {
			t.Fatalf("namespace child %q did not reverse through projection", original)
		}
	}
}

func TestAttemptToolProjectionPrefersUnoccupiedSafeNamespacedLeaf(t *testing.T) {
	namespaced, _ := canonical.NewToolKey("mcp/filesystem", canonical.ToolKindFunction, "read_file")
	request := projectionTestRequest(t, namespaced)

	_, table, decisions, err := ProjectAttemptTools(request)
	if err != nil {
		t.Fatal(err)
	}
	if wire, _ := table.WireName(namespaced); wire != "read_file" {
		t.Fatalf("safe namespaced leaf = %q, want read_file", wire)
	}
	if len(decisions) != 0 {
		t.Fatalf("safe unoccupied leaf emitted compatibility decisions: %#v", decisions)
	}
}

func TestAttemptToolProjectionPreservesWebSearch(t *testing.T) {
	functionKey, _ := canonical.NewToolKey(canonical.ToolNamespaceRequest, canonical.ToolKindFunction, "lookup")
	functionRequest := projectionTestRequest(t, functionKey)
	declarations := append(canonicaltest.Tools(functionRequest), canonical.NewWebSearchDeclaration())
	set, err := canonical.NewToolSet(declarations)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := canonical.NewToolCallID("search_call")
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{
		Action: canonical.WebSearchActionSearch, Queries: []string{"one"},
	})
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	declarationItem, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{declarationItem, call}})

	projected, table, _, err := ProjectAttemptTools(request)
	if err != nil {
		t.Fatal(err)
	}
	tools := canonicaltest.Tools(projected)
	if len(tools) != 2 || tools[1].Kind() != canonical.ToolKindWebSearch || tools[1].Key() != canonical.WebSearchToolKey() {
		t.Fatalf("projected tools = %#v", tools)
	}
	if wire, err := table.WireName(canonical.WebSearchToolKey()); err != nil || wire != canonical.ToolTypeWebSearch {
		t.Fatalf("web-search projection = %q, %v", wire, err)
	}
	projectedCall, ok := projected.Items()[1].ToolCall()
	if !ok || projectedCall.Tool() != canonical.WebSearchToolKey() || projectedCall.CallID() != callID {
		t.Fatalf("projected web-search call = %#v", projected.Items()[1])
	}
}
