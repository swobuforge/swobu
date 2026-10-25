package canonical

import (
	"strconv"
	"testing"
)

// representativeSchema is sized like a real function-tool input schema
// (properties, types, descriptions). Kept constant so the benchmark measures
// clone/alloc overhead, not schema parsing.
const representativeSchema = `{"type":"object","properties":{"query":{"type":"string","description":"search query"},"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string"},"filters":{"type":"object","properties":{"kind":{"type":"string","enum":["a","b","c"]},"after":{"type":"string"}},"additionalProperties":false}},"required":["query"],"additionalProperties":false}`

// toolEnvironmentRequest builds a CanonicalRequest whose EffectiveTools exercises
// the full clone path: many flat function tools, a namespace with children,
// and an MCP source with a function catalog. It is shared by the benchmark and
// the relational identity test so they measure the same shape.
func toolEnvironmentRequest(tb testing.TB, flatCount int) CanonicalRequest {
	tb.Helper()
	schema := toolEnvironmentSchema()
	var flat []ToolDeclaration
	for i := range flatCount {
		key, err := NewRequestToolKey(ToolKindFunction, "fn"+strconv.Itoa(i))
		if err != nil {
			tb.Fatal(err)
		}
		decl, err := NewFunctionTool(key, "function tool "+strconv.Itoa(i), schema, SchemaContract{Profile: SchemaProfileAnthropic})
		if err != nil {
			tb.Fatal(err)
		}
		flat = append(flat, decl)
	}
	// Namespace with its own children (recursive clone cost).
	nsChildA := toolEnvironmentFunction(tb, "ns_a", "namespace child a", schema)
	nsChildB := toolEnvironmentFunction(tb, "ns_b", "namespace child b", schema)
	nsKey, err := NewToolKey("area", ToolKindNamespace, "search")
	if err != nil {
		tb.Fatal(err)
	}
	namespace, err := NewToolNamespace(nsKey, "grouped search tools", []ToolDeclaration{nsChildA, nsChildB})
	if err != nil {
		tb.Fatal(err)
	}
	// MCP source with a function catalog (another recursive clone cost).
	mcpChild := toolEnvironmentFunction(tb, "remote_get", "remote callable", schema)
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
// across flat, namespaced and remote tool declarations.
func BenchmarkEffectiveTools(b *testing.B) {
	for _, flatCount := range []int{8, 32, 128} {
		request := toolEnvironmentRequest(b, flatCount)
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

func toolEnvironmentSchema() ToolSchema {
	object, err := ParseJSONObject([]byte(representativeSchema))
	if err != nil {
		panic(err)
	}
	return NewToolSchemaObject(object)
}

func toolEnvironmentFunction(tb testing.TB, name, description string, schema ToolSchema) ToolDeclaration {
	tb.Helper()
	key, err := NewRequestToolKey(ToolKindFunction, name)
	if err != nil {
		tb.Fatal(err)
	}
	decl, err := NewFunctionTool(key, description, schema, SchemaContract{Profile: SchemaProfileAnthropic})
	if err != nil {
		tb.Fatal(err)
	}
	return decl
}
