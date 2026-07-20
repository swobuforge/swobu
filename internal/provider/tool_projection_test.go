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
	for _, declaration := range projected.Tools() {
		name := declaration.Key().Name()
		if len(name) > toolname.MaxLength || !toolname.Safe(name) {
			t.Fatalf("unsafe projection %q", name)
		}
		if seen[name] {
			t.Fatalf("projection collision %q", name)
		}
		seen[name] = true
		original, ok := table.OriginalKey(declaration.Key())
		if !ok || original != request.Tools()[len(seen)-1].Key() {
			t.Fatalf("projection did not round-trip: %#v", declaration.Key())
		}
	}
	if wire, _ := table.WireName(plain); wire != "lookup" {
		t.Fatalf("safe request name = %q, want preserved", wire)
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
	return canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Tools: canonical.Specify(set)})
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
	if len(projected.Tools()) != 2 || projected.Tools()[0].Key().Name() == projected.Tools()[1].Key().Name() {
		t.Fatal("projection allocator did not avoid a reserved literal name")
	}
	for _, declaration := range projected.Tools() {
		if _, ok := table.OriginalKey(declaration.Key()); !ok {
			t.Fatalf("projection lost reverse mapping for %q", declaration.Key().Name())
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
