package canonical

import "testing"

// TestEffectiveToolsByteIdentity is the falsification guard for epic-50 task
// 070. The fix removes redundant internal clones from ToolEnvironmentAt,
// sharing the single boundary-detached declaration value between the byKey map
// and the ordered slice. This test pins the observable result of that build to
// byte-equality: ordering, every key, every kind, every function schema, and
// the namespace + MCP child keys must be identical to what the constructors
// produced. If a future change to the build path silently drops, reorders, or
// aliases a declaration, this test fails. It makes no assumptions about clone
// *count* — only about the *result*.
func TestEffectiveToolsByteIdentity(t *testing.T) {
	const flatCount = 4
	request := requestFixtureFor70(t, flatCount)

	environment, err := EffectiveTools(request)
	if err != nil {
		t.Fatalf("EffectiveTools: %v", err)
	}

	// Expected top-level ordering: the flat function tools (0..flatCount-1),
	// then the namespace, then the MCP source — matching requestFixtureFor70.
	declarations := environment.Declarations()
	wantTopLevel := flatCount + 2 // namespace + MCP source
	if len(declarations) != wantTopLevel {
		t.Fatalf("top-level declaration count: got %d, want %d", len(declarations), wantTopLevel)
	}

	// Every flat function tool keeps its canonical key, kind, and schema bytes.
	for i := range flatCount {
		decl := declarations[i]
		if got := decl.Kind(); got != ToolKindFunction {
			t.Fatalf("flat tool %d kind: got %q, want function", i, got)
		}
		wantName := "fn" + itoaFor70(i)
		if got := decl.Key().Name(); got != wantName {
			t.Fatalf("flat tool %d name: got %q, want %q", i, got, wantName)
		}
		fn, ok := decl.Function()
		if !ok {
			t.Fatalf("flat tool %d: Function() missing", i)
		}
		if got, want := fn.InputSchema().RawObject(), mustSchemaFor70().RawObject(); got != want {
			t.Fatalf("flat tool %d schema bytes diverged", i)
		}
	}

	// Namespace: present at position flatCount, with both children resolvable
	// by full key through Lookup (proves observe walked namespace.Tools()).
	namespace := declarations[flatCount]
	if namespace.Kind() != ToolKindNamespace {
		t.Fatalf("namespace position kind: got %q, want namespace", namespace.Kind())
	}
	if got, want := namespace.Key().Name(), "search"; got != want {
		t.Fatalf("namespace name: got %q, want %q", got, want)
	}
	assertChildResolvable(t, &environment, ToolNamespaceRequest, "ns_a")
	assertChildResolvable(t, &environment, ToolNamespaceRequest, "ns_b")

	// MCP source: present at the last position, with its catalog child
	// resolvable (proves observe walked MCPToolSource.Tools()).
	source := declarations[flatCount+1]
	if source.Kind() != ToolKindMCP {
		t.Fatalf("mcp position kind: got %q, want mcp", source.Kind())
	}
	if got, want := source.Key().Name(), "filesystem"; got != want {
		t.Fatalf("mcp name: got %q, want %q", got, want)
	}
	assertChildResolvable(t, &environment, ToolNamespaceRequest, "remote_get")

	// Idempotent redeclaration with the same owner is preserved (not flagged
	// ambiguous): the same declaration-set declared twice must not error and
	// must keep cardinality stable. Equivalent() compares description and
	// schema bytes, so the duplicate must be genuinely identical.
	schema := mustSchemaFor70()
	original := mustFunctionFor70(t, "fn0", "function tool 0", schema)
	firstItem, err := NewToolDeclarationsItem(mustToolSetFor70(t, original), ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := mustFunctionFor70(t, "fn0", "function tool 0", schema)
	dupItem, err := NewToolDeclarationsItem(mustToolSetFor70(t, duplicate), ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	dupRequest := NewCanonicalRequest(RequestParams{
		Model: Specify("test-model"),
		Items: []CanonicalItem{firstItem, dupItem},
	})
	dupEnv, err := EffectiveTools(dupRequest)
	if err != nil {
		t.Fatalf("idempotent redeclaration should not error: %v", err)
	}
	if got := len(dupEnv.Declarations()); got != 1 {
		t.Fatalf("idempotent redeclaration cardinality: got %d, want 1", got)
	}
}

// TestEffectiveToolsAmbiguityPreserved confirms 070 did not weaken the
// ambiguity invariant: two declarations with the same key but different owners
// still fail. The fix removed clones, not validation.
func TestEffectiveToolsAmbiguityPreserved(t *testing.T) {
	schema := mustSchemaFor70()
	fn := mustFunctionFor70(t, "shared", "", schema)
	item, err := NewToolDeclarationsItem(mustToolSetFor70(t, fn), ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	// A namespace that re-declares the same leaf tool: observe must see the
	// child under a different parent and reject as ambiguous.
	nsChild := mustFunctionFor70(t, "shared", "", schema)
	nsKey, err := NewToolKey("conflict", ToolKindNamespace, "grp")
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := NewToolNamespace(nsKey, "", []ToolDeclaration{nsChild})
	if err != nil {
		t.Fatal(err)
	}
	nsItem, err := NewToolDeclarationsItem(mustToolSetFor70(t, namespace), ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	request := NewCanonicalRequest(RequestParams{
		Model: Specify("test-model"),
		Items: []CanonicalItem{item, nsItem},
	})
	if _, err := EffectiveTools(request); err == nil {
		t.Fatal("expected ambiguity error for same key under different owners, got nil")
	}
}

func assertChildResolvable(t *testing.T, env *ToolEnvironment, namespace, name string) {
	t.Helper()
	key, err := NewToolKey(namespace, ToolKindFunction, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := env.Lookup(key); !ok {
		t.Fatalf("namespace/mcp child %s/%s not resolvable via Lookup", namespace, name)
	}
}

func mustToolSetFor70(t *testing.T, declarations ...ToolDeclaration) ToolSet {
	t.Helper()
	set, err := NewToolSet(declarations)
	if err != nil {
		t.Fatal(err)
	}
	return set
}
