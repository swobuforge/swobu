package wire

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestPrepareFlatToolSetPreservesNestedOrder(t *testing.T) {
	first := canonicaltest.MustFunctionTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "group/first"),
		"", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool](),
	)
	second := canonicaltest.MustCustomTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "group/second"),
		"", canonical.EmptyToolFormat(),
	)
	nestedKey := canonicaltest.MustRequestToolKey(canonical.ToolKindNamespace, "nested")
	nested, err := canonical.NewToolNamespace(nestedKey, "", []canonical.ToolDeclaration{second})
	if err != nil {
		t.Fatal(err)
	}
	parentKey := canonicaltest.MustRequestToolKey(canonical.ToolKindNamespace, "group")
	parent, err := canonical.NewToolNamespace(parentKey, "", []canonical.ToolDeclaration{first, nested})
	if err != nil {
		t.Fatal(err)
	}

	got, err := PrepareFlatToolSet([]canonical.ToolDeclaration{parent}, func(tool canonical.ToolDeclaration) string {
		return string(tool.Kind()) + "\x00" + tool.Key().Name()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Declarations) != 2 || got.Declarations[0].Key() != first.Key() || got.Declarations[1].Key() != second.Key() {
		t.Fatalf("flattened declarations = %#v, want ordered callable descendants", got)
	}
	if got.RemovedNamespaces != 2 {
		t.Fatalf("flattened namespace count = %d, want 2", got.RemovedNamespaces)
	}
}

func TestPrepareFlatToolSetRejectsTargetIdentityCollision(t *testing.T) {
	first := canonicaltest.MustFunctionTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "one/lookup"),
		"", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool](),
	)
	second := canonicaltest.MustFunctionTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "two/lookup"),
		"", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool](),
	)

	if _, err := PrepareFlatToolSet([]canonical.ToolDeclaration{first, second}, func(canonical.ToolDeclaration) string {
		return "lookup"
	}); err == nil {
		t.Fatal("flat tool set accepted colliding target identities")
	}
}
