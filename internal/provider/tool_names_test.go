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
		resolved, ok := first.CanonicalKey(one)
		if !ok || resolved != key {
			t.Fatalf("reverse lookup = %q, %t", resolved, ok)
		}
	}
}

func TestAttemptToolNamesAreSetIndependent(t *testing.T) {
	stable, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	unrelated, _ := canonical.NewToolKey("mcp/github", canonical.ToolKindFunction, "lookup")
	one, _, err := BuildAttemptToolNames(toolNamesTestRequest(t, stable))
	if err != nil {
		t.Fatal(err)
	}
	two, _, err := BuildAttemptToolNames(toolNamesTestRequest(t, unrelated, stable))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := one.WireName(stable)
	second, _ := two.WireName(stable)
	if first != second {
		t.Fatalf("wire name changed after unrelated declaration: %q != %q", first, second)
	}
}

func TestAttemptToolNamesKeepSameNameAcrossCallableKindsDistinct(t *testing.T) {
	function, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "apply")
	custom, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindCustom, "apply")
	names, _, err := BuildAttemptToolNames(toolNamesTestRequest(t, function, custom))
	if err != nil {
		t.Fatal(err)
	}
	functionName, _ := names.WireName(function)
	customName, _ := names.WireName(custom)
	if functionName == customName {
		t.Fatalf("cross-kind wire names collide at %q", functionName)
	}
	for _, test := range []struct {
		name string
		want canonical.ToolKey
	}{
		{name: functionName, want: function},
		{name: customName, want: custom},
	} {
		got, ok := names.CanonicalKey(test.name)
		if !ok || got != test.want {
			t.Fatalf("reverse lookup %q = %q, %t", test.name, got, ok)
		}
	}
}

func TestAttemptToolNamesCrossKindCollisionIsOrderIndependent(t *testing.T) {
	function, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, canonical.ToolDiscoveryKey().Name())
	discovery := canonical.ToolDiscoveryKey()
	first, _, err := BuildAttemptToolNames(mixedToolNamesTestRequest(t, function, true))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := BuildAttemptToolNames(mixedToolNamesTestRequest(t, function, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []canonical.ToolKey{function, discovery} {
		left, _ := first.WireName(key)
		right, _ := second.WireName(key)
		if left != right {
			t.Fatalf("wire name for %q changed with order: %q != %q", key, left, right)
		}
	}
}

func TestProviderDiscoveryDoesNotAffectOrdinaryWireNames(t *testing.T) {
	functionKey, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, canonical.ToolDiscoveryKey().Name())
	function := canonicaltest.MustFunctionTool(functionKey, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	discovery, err := canonical.NewToolDiscoveryTool("find tools", canonicaltest.Schema(t, `{"type":"object"}`), canonical.DiscoveryExecutorProvider)
	if err != nil {
		t.Fatal(err)
	}
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, function})
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	names, changes, err := BuildAttemptToolNames(canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}}))
	if err != nil {
		t.Fatal(err)
	}
	wireName, err := names.WireName(functionKey)
	if err != nil {
		t.Fatal(err)
	}
	if wireName != functionKey.Name() {
		t.Fatalf("function wire name = %q, want literal %q", wireName, functionKey.Name())
	}
	if _, err := names.WireName(discovery.Key()); err == nil {
		t.Fatal("provider discovery received an unused ordinary callable alias")
	}
	if len(changes) != 0 {
		t.Fatalf("provider discovery caused naming changes: %#v", changes)
	}
}

func mixedToolNamesTestRequest(t *testing.T, functionKey canonical.ToolKey, functionFirst bool) canonical.CanonicalRequest {
	t.Helper()
	function := canonicaltest.MustFunctionTool(functionKey, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	discovery, err := canonical.NewToolDiscoveryTool("find tools", canonicaltest.Schema(t, `{"type":"object"}`), canonical.DiscoveryExecutorClient)
	if err != nil {
		t.Fatal(err)
	}
	declarations := []canonical.ToolDeclaration{discovery, function}
	if functionFirst {
		declarations = []canonical.ToolDeclaration{function, discovery}
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

func TestAttemptToolNamesRejectInjectedGeneratedCollisionWithoutOrdinalFallback(t *testing.T) {
	left, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	right, _ := canonical.NewToolKey("mcp/github", canonical.ToolKindFunction, "search")
	_, _, err := buildAttemptToolNames(
		toolNamesTestRequest(t, left, right),
		func(string, []string, string) string { return "s__forced__collision" },
	)
	if err == nil {
		t.Fatal("attempt names accepted an injected generated-name collision")
	}
}

func TestAttemptToolNamesRebuildIdenticallyAfterRestart(t *testing.T) {
	key, _ := canonical.NewToolKey("mcp/документы", canonical.ToolKindFunction, strings.Repeat("поиск ", 20))
	request := toolNamesTestRequest(t, key)
	before, _, err := BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	after, _, err := BuildAttemptToolNames(request.Clone())
	if err != nil {
		t.Fatal(err)
	}
	first, _ := before.WireName(key)
	second, _ := after.WireName(key)
	if first != second || len(first) > toolname.MaxLength || !toolname.Safe(first) {
		t.Fatalf("restart names = %q and %q", first, second)
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
	wireName, err := names.WireName(historical)
	if err != nil {
		t.Fatalf("historical call missing: %v", err)
	}
	rebuilt, _, err := BuildAttemptToolNames(request.Clone())
	if err != nil {
		t.Fatal(err)
	}
	rebuiltName, _ := rebuilt.WireName(historical)
	if rebuiltName != wireName {
		t.Fatalf("historical wire name changed after rebuild: %q != %q", wireName, rebuiltName)
	}
}

func TestAttemptToolNamesDoNotFlattenSpecificChildUnderResidualMCP(t *testing.T) {
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	childKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	child := canonicaltest.MustFunctionTool(childKey, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	source, _ := canonical.NewMCPConnectorSource(
		"connector_docs", canonical.Unspecified[[]string](), canonical.NewMCPApprovalNever(),
		canonical.MCPLoadingDeferred, canonical.Unspecified[[]string](),
	)
	declaration, _ := canonical.NewMCPToolSource(sourceKey, "", source, []canonical.ToolDeclaration{child})
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: []canonical.CanonicalItem{item},
		ToolPolicy: canonical.Specify(
			canonical.NewToolPolicy(canonical.ToolPolicySpecific, &childKey),
		),
	})

	names, _, err := BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := names.WireName(childKey); err == nil {
		t.Fatal("unmaterialized MCP child received a flat callable alias")
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
		switch key.Kind() {
		case canonical.ToolKindFunction:
			declarations[index] = canonicaltest.MustFunctionTool(key, "", canonical.NewToolSchemaObject(canonicaltest.Object(t, `{"type":"object"}`)), canonical.Unspecified[bool]())
		case canonical.ToolKindCustom:
			declarations[index] = canonicaltest.MustCustomTool(key, "", canonical.EmptyToolFormat())
		default:
			t.Fatalf("unsupported test tool kind %q", key.Kind())
		}
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
