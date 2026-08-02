package canonical

import (
	"testing"
)

// This file holds the evidence artifacts for epic-50 task 070
// (tool-declaration clone amplification). The per-request tool environment
// build (EffectiveTools / ToolEnvironmentAt) was the #1 allocator on the live
// daemon alloc profile: JSONObject.Bytes = 294.86 MB (22.82%), driven by
// cloneToolDeclarations re-copying each declaration's JSON schema ~4x per build.

// representativeSchema is sized like a real function-tool input schema
// (properties, types, descriptions). Kept constant so the benchmark measures
// clone/alloc overhead, not schema parsing.
const representativeSchema = `{"type":"object","properties":{"query":{"type":"string","description":"search query"},"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string"},"filters":{"type":"object","properties":{"kind":{"type":"string","enum":["a","b","c"]},"after":{"type":"string"}},"additionalProperties":false}},"required":["query"],"additionalProperties":false}`

// requestFixtureFor70 builds a CanonicalRequest whose EffectiveTools exercises
// the full clone path: many flat function tools, a namespace with children,
// and an MCP source with a function catalog. It is shared by the benchmark and
// the relational identity test so they measure the same shape.
func requestFixtureFor70(tb testing.TB, flatCount int) CanonicalRequest {
	tb.Helper()
	schema := mustSchemaFor70()
	var flat []ToolDeclaration
	for i := range flatCount {
		key, err := NewRequestToolKey(ToolKindFunction, "fn"+itoaFor70(i))
		if err != nil {
			tb.Fatal(err)
		}
		decl, err := NewFunctionTool(key, "function tool "+itoaFor70(i), schema, Unspecified[bool]())
		if err != nil {
			tb.Fatal(err)
		}
		flat = append(flat, decl)
	}
	// Namespace with its own children (recursive clone cost).
	nsChildA := mustFunctionFor70(tb, "ns_a", "namespace child a", schema)
	nsChildB := mustFunctionFor70(tb, "ns_b", "namespace child b", schema)
	nsKey, err := NewToolKey("area", ToolKindNamespace, "search")
	if err != nil {
		tb.Fatal(err)
	}
	namespace, err := NewToolNamespace(nsKey, "grouped search tools", []ToolDeclaration{nsChildA, nsChildB})
	if err != nil {
		tb.Fatal(err)
	}
	// MCP source with a function catalog (another recursive clone cost).
	mcpChild := mustFunctionFor70(tb, "remote_get", "remote callable", schema)
	mcpKey, err := NewToolKey("remote", ToolKindMCP, "filesystem")
	if err != nil {
		tb.Fatal(err)
	}
	mcpSource, err := NewMCPURLSource("https://example.test/mcp", Unspecified[[]string](), NewMCPApprovalNever(), MCPLoadingEager, Unspecified[[]string]())
	if err != nil {
		tb.Fatal(err)
	}
	source, err := NewMCPToolSource(mcpKey, "remote filesystem server", mcpSource, []ToolDeclaration{mcpChild})
	if err != nil {
		tb.Fatal(err)
	}
	flat = append(flat, namespace, source)

	set, err := NewToolSet(flat)
	if err != nil {
		tb.Fatal(err)
	}
	declarationsItem, err := NewToolDeclarationsItem(set, ContextScopeRequest)
	if err != nil {
		tb.Fatal(err)
	}
	return NewCanonicalRequest(RequestParams{
		Model: Specify("test-model"),
		Items: []CanonicalItem{declarationsItem},
	})
}

// BenchmarkEffectiveTools measures the per-request tool-environment build cost
// across tool counts representative of real daemon traffic. Before 070 each
// declaration was deep-cloned ~4x per build (boundary accessor + observe + add +
// NewToolSet). 070 collapses the internal clones to one detach at the boundary.
func BenchmarkEffectiveTools(b *testing.B) {
	for _, flatCount := range []int{8, 32, 128} {
		request := requestFixtureFor70(b, flatCount)
		b.Run(nameForToolCount70(flatCount), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				environment, err := EffectiveTools(request)
				if err != nil {
					b.Fatal(err)
				}
				// Declarations() is the read consumers actually perform; include
				// it so the benchmark reflects the full build+read cost.
				if environment.IsEmpty() {
					b.Fatal("expected non-empty tool environment")
				}
				_ = environment.Declarations()
			}
		})
	}
}

func nameForToolCount70(n int) string {
	switch n {
	case 8:
		return "FewFlat"
	case 32:
		return "ManyFlat"
	default:
		return "ManyTools"
	}
}

func itoaFor70(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func mustSchemaFor70() ToolSchema {
	object, err := ParseJSONObject([]byte(representativeSchema))
	if err != nil {
		panic(err)
	}
	return NewToolSchemaObject(object)
}

func mustFunctionFor70(tb testing.TB, name, description string, schema ToolSchema) ToolDeclaration {
	tb.Helper()
	key, err := NewRequestToolKey(ToolKindFunction, name)
	if err != nil {
		tb.Fatal(err)
	}
	decl, err := NewFunctionTool(key, description, schema, Unspecified[bool]())
	if err != nil {
		tb.Fatal(err)
	}
	return decl
}
