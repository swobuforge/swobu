package canonical_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestProjectionBasisContainsActualClosedCanonicalDiscriminants(t *testing.T) {
	covered := make(map[string][]string)
	for _, witness := range canonicaltest.ProjectionBasis(t, "model") {
		actual, err := inspectProjectionWitness(witness.Request)
		if err != nil {
			t.Fatalf("inspect projection witness %q: %v", witness.Name, err)
		}
		for category, values := range actual {
			covered[category] = append(covered[category], values...)
		}
	}

	expected := map[string][]string{
		"ItemKind":             canonicalConstValues(t, "item.go", "ItemKind"),
		"ToolKind":             withoutValue(canonicalConstValues(t, "tool_ontology.go", "ToolKind"), string(canonical.ToolKindMCP)),
		"MessageRole":          canonicalConstValues(t, "item.go", "MessageRole"),
		"MessagePart":          canonicalConstValues(t, "content_part.go", "PartKind"),
		"ToolResultPart":       canonicalConstValues(t, "content_part.go", "PartKind"),
		"ReasoningPartKind":    canonicalConstValues(t, "item.go", "ReasoningPartKind"),
		"ReasoningComputeKind": canonicalConstValues(t, "reasoning.go", "ReasoningComputeKind"),
		"OutputFormatKind":     canonicalConstValues(t, "output_format.go", "OutputFormatKind"),
		"ToolPolicyMode":       canonicalConstValues(t, "tool_policy.go", "ToolPolicyMode"),
		"InferenceEffort":      canonicalConstValues(t, "reasoning.go", "InferenceEffort"),
		"ReasoningDisclosure":  canonicalConstValues(t, "reasoning.go", "ReasoningDisclosure"),
		"ResponsesContext":     canonicalConstValues(t, "reasoning.go", "ResponsesReasoningContext"),
		"ToolCallBatchMode":    canonicalConstValues(t, "tool_call_batch.go", "ToolCallBatchMode"),
		"DiscoveryExecutor":    canonicalConstValues(t, "tool_declarations.go", "DiscoveryExecutor"),
		"DiscoveryQueryKind":   canonicalConstValues(t, "tool_declarations.go", "ToolDiscoveryQueryKind"),
		"WebSearchAction":      canonicalConstValues(t, "web_search_lifecycle.go", "WebSearchAction"),
	}
	for category, want := range expected {
		got := uniqueSorted(covered[category])
		if !equalStrings(got, want) {
			t.Errorf("%s projection coverage = %q, want closed canonical values %q", category, got, want)
		}
	}

	wantRelations := []string{
		"mixed callable kinds",
		"open call",
		"parallel calls",
		"parallel settled effects",
		"search open",
		"search settled",
		"settled call/result",
	}
	if got := uniqueSorted(covered["Relation"]); !equalStrings(got, wantRelations) {
		t.Errorf("relation projection coverage = %v, want %v", got, wantRelations)
	}
}

func inspectProjectionWitness(request canonical.CanonicalRequest) (map[string][]string, error) {
	observed := make(map[string][]string)
	add := func(category string, value any) {
		observed[category] = append(observed[category], fmt.Sprint(value))
	}
	var inspectDeclaration func(canonical.ToolDeclaration)
	inspectDeclaration = func(declaration canonical.ToolDeclaration) {
		add("ToolKind", declaration.Kind())
		if discovery, ok := declaration.Discovery(); ok {
			add("DiscoveryExecutor", discovery.Executor())
			add("DiscoveryQueryKind", discovery.QueryKind())
		}
		if namespace, ok := declaration.Namespace(); ok {
			for _, child := range namespace.Tools() {
				inspectDeclaration(child)
			}
		}
	}

	items := request.Items()
	for _, item := range items {
		add("ItemKind", item.Kind())
		if message, ok := item.Message(); ok {
			add("MessageRole", message.Role())
			for _, part := range message.Content() {
				add("MessagePart", part.Kind())
			}
		}
		if declarations, ok := item.ToolDeclarations(); ok {
			for _, declaration := range declarations.Tools().Declarations() {
				inspectDeclaration(declaration)
			}
		}
		if call, ok := item.ToolCall(); ok {
			add("ToolKind", call.Tool().Kind())
			if search, ok := call.Input().WebSearch(); ok {
				add("WebSearchAction", search.Action)
			}
			if executor, specified := call.DiscoveryExecutor(); specified {
				add("DiscoveryExecutor", executor)
			}
		}
		if result, ok := item.ToolResult(); ok {
			for _, part := range result.Content() {
				add("ToolResultPart", part.Kind())
			}
		}
		if result, ok := item.ToolDiscoveryResult(); ok {
			add("DiscoveryExecutor", result.Executor())
			for _, declaration := range result.Tools().Declarations() {
				inspectDeclaration(declaration)
			}
		}
		if reasoning, ok := item.Reasoning(); ok {
			for _, part := range reasoning.Parts() {
				add("ReasoningPartKind", part.Kind())
			}
		}
	}

	if compute, specified := request.Reasoning().ComputeField().Get(); specified {
		add("ReasoningComputeKind", compute.Kind())
	}
	if disclosure, specified := request.Reasoning().DisclosureField().Get(); specified {
		add("ReasoningDisclosure", disclosure)
	}
	if reasoningContext, specified := request.Reasoning().ResponsesContextField().Get(); specified {
		add("ResponsesContext", reasoningContext)
	}
	add("OutputFormatKind", request.OutputFormat().Kind)
	if request.ToolPolicySpecified() {
		add("ToolPolicyMode", request.ToolPolicy().Mode)
	}
	if effort, specified := request.Controls().Effort.Get(); specified {
		add("InferenceEffort", effort)
	}
	add("ToolCallBatchMode", request.ToolCallBatch().Mode)

	effects, err := canonical.MatchToolEffects(items)
	if err != nil {
		return nil, err
	}
	inspectEffectRelations(observed, effects)
	return observed, nil
}

func inspectEffectRelations(observed map[string][]string, effects []canonical.ToolEffect) {
	if len(effects) == 0 {
		return
	}
	kinds := make(map[canonical.ToolKind]struct{})
	open := false
	settled := false
	searchOpen := false
	searchSettled := false
	for _, effect := range effects {
		kinds[effect.Kind] = struct{}{}
		if effect.ResultIndex < 0 {
			open = true
			searchOpen = searchOpen || effect.Kind == canonical.ToolKindWebSearch
			continue
		}
		settled = true
		searchSettled = searchSettled || effect.Kind == canonical.ToolKindWebSearch
	}
	addRelation := func(value string, present bool) {
		if present {
			observed["Relation"] = append(observed["Relation"], value)
		}
	}
	addRelation("open call", open)
	addRelation("settled call/result", settled)
	addRelation("search open", searchOpen)
	addRelation("search settled", searchSettled)
	addRelation("mixed callable kinds", len(kinds) > 1)

	parallel := false
	parallelSettled := false
	for left := 0; left < len(effects); left++ {
		for right := left + 1; right < len(effects); right++ {
			leftOpenWhenRightCalled := effects[left].ResultIndex < 0 || effects[right].CallIndex < effects[left].ResultIndex
			if !leftOpenWhenRightCalled {
				continue
			}
			parallel = true
			if effects[left].ResultIndex >= 0 && effects[right].ResultIndex >= 0 {
				parallelSettled = true
			}
		}
	}
	addRelation("parallel calls", parallel)
	addRelation("parallel settled effects", parallelSettled)
}

func canonicalConstValues(t *testing.T, fileName, typeName string) []string {
	t.Helper()
	path := filepath.Join(".", fileName)
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok || generic.Tok != token.CONST {
			continue
		}
		activeType := ""
		ordinal := 0
		for _, spec := range generic.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if identifier, ok := value.Type.(*ast.Ident); ok {
				activeType = identifier.Name
			}
			if activeType != typeName {
				continue
			}
			for index := range value.Names {
				values = append(values, canonicalConstValue(t, value, index, ordinal))
				ordinal++
			}
		}
	}
	return uniqueSorted(values)
}

func canonicalConstValue(t *testing.T, spec *ast.ValueSpec, index, ordinal int) string {
	t.Helper()
	if len(spec.Values) == 0 {
		return strconv.Itoa(ordinal + 1)
	}
	expression := spec.Values[index]
	if len(spec.Values) == 1 {
		expression = spec.Values[0]
	}
	switch value := expression.(type) {
	case *ast.BasicLit:
		unquoted, err := strconv.Unquote(value.Value)
		if err != nil {
			t.Fatal(err)
		}
		return unquoted
	case *ast.BinaryExpr:
		if identifier, ok := value.X.(*ast.Ident); ok && identifier.Name == "iota" && value.Op == token.ADD {
			literal, ok := value.Y.(*ast.BasicLit)
			if !ok {
				t.Fatalf("unsupported canonical const expression %T", value.Y)
			}
			offset, err := strconv.Atoi(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			return strconv.Itoa(ordinal + offset)
		}
	}
	t.Fatalf("unsupported canonical const expression %T", expression)
	return ""
}

func withoutValue(values []string, excluded string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != excluded {
			out = append(out, value)
		}
	}
	return out
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
