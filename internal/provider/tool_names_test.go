package provider

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire/toolname"
)

func TestAttemptToolNamesAreOrderIndependentAndReversible(t *testing.T) {
	left, _ := canonical.NewToolKey("a-b", canonical.ToolKindFunction, "same")
	right, _ := canonical.NewToolKey("a_b", canonical.ToolKindFunction, "same")
	first, _, err := BuildAttemptToolNames(toolNamesTestRequest(t, left, right))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := BuildAttemptToolNames(toolNamesTestRequest(t, right, left))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []canonical.ToolKey{left, right} {
		one, err := first.WireName(key)
		if err != nil {
			t.Fatal(err)
		}
		two, err := second.WireName(key)
		if err != nil {
			t.Fatal(err)
		}
		if one != two {
			t.Fatalf("wire name for %q changed with order: %q != %q", key, one, two)
		}
		if len(one) > toolname.MaxLength || !toolname.Safe(one) {
			t.Fatalf("unsafe wire name %q", one)
		}
		resolved, ok := first.CanonicalKey(key.Kind(), one)
		if !ok || resolved != key {
			t.Fatalf("reverse lookup = %q, %t", resolved, ok)
		}
	}
}

func TestAttemptToolNamesPreserveOnlySafeUnreservedRequestLiterals(t *testing.T) {
	plain, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	reserved, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "s__literal")
	unsafe, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "look up")
	names, changes, err := BuildAttemptToolNames(toolNamesTestRequest(t, plain, reserved, unsafe))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := names.WireName(plain); got != "lookup" {
		t.Fatalf("plain literal = %q", got)
	}
	for _, key := range []canonical.ToolKey{reserved, unsafe} {
		got, err := names.WireName(key)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, toolname.GeneratedPrefix) || !toolname.Safe(got) {
			t.Fatalf("generated name = %q", got)
		}
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %d, want 2", len(changes))
	}
}

func TestAttemptToolNamesIncludeHistoricalCalls(t *testing.T) {
	historical, _ := canonical.NewToolKey("history/old", canonical.ToolKindCustom, "apply")
	callID, _ := canonical.NewToolCallID("call_old")
	call, _ := canonical.NewToolCallItem(callID, historical, canonical.NewTextToolInput("x"))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{call}})
	names, _, err := BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := names.WireName(historical); err != nil {
		t.Fatalf("historical call missing: %v", err)
	}
}

func TestAttemptToolNamesPreserveSemanticMCPScope(t *testing.T) {
	key, _ := canonical.NewToolKey("mcp/admin", canonical.ToolKindFunction, "delete")
	names, _, err := BuildAttemptToolNames(toolNamesTestRequest(t, key))
	if err != nil {
		t.Fatal(err)
	}
	wireName, err := names.WireName(key)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(wireName, toolname.GeneratedPrefix+"mcp__admin__delete__") {
		t.Fatalf("wire name lost semantic mcp scope: %q", wireName)
	}
}

func toolNamesTestRequest(t *testing.T, keys ...canonical.ToolKey) canonical.CanonicalRequest {
	t.Helper()
	declarations := make([]canonical.ToolDeclaration, len(keys))
	for index, key := range keys {
		declarations[index] = canonicaltest.MustFunctionTool(key, "", canonical.NewToolSchemaObject(canonicaltest.Object(t, `{"type":"object"}`)), canonical.Unspecified[bool]())
	}
	set, err := canonical.NewToolSet(declarations)
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}})
}
