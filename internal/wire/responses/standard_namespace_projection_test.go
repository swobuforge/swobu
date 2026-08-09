package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestFlatResponsesFlattensNamespaceInsteadOfDroppingCallableChildren(t *testing.T) {
	request := responsesFunctionNamespaceRequest(t)
	topLevelTools := canonicaltest.Tools(request)
	topLevelFunctions := 0
	for _, declaration := range topLevelTools {
		if declaration.Kind() == canonical.ToolKindFunction {
			topLevelFunctions++
		}
	}
	if topLevelFunctions != 0 {
		t.Fatalf("top-level function count = %d, want 0 before namespace flattening", topLevelFunctions)
	}
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	child := topLevelTools[0]
	namespace, _ := child.Namespace()
	wireName, _ := names.WireName(namespace.Tools()[0].Key())
	document, err := LowerProviderRequestDocument(
		EncodeInput{Request: request, ToolNames: names},
		delivery.BufferedDelivery(),
		nil,
		"exchange",
		EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Tools) != 1 || document.Tools[0].Type != "function" || document.Tools[0].Name != wireName {
		t.Fatalf("Responses tools = %#v, want flat %s function", document.Tools, wireName)
	}
}

func TestOfficialResponsesLoweringKeepsDuplicateNamespaceLeavesDistinct(t *testing.T) {
	request := responsesDuplicateLeafNamespacesRequest(t)
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, err := LowerProviderRequestDocument(
		EncodeInput{Request: request, ToolNames: names},
		delivery.BufferedDelivery(),
		nil,
		"exchange",
		EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Tools) != 2 {
		t.Fatalf("Responses tools = %#v, want two functions", document.Tools)
	}
	if document.Tools[0].Name == document.Tools[1].Name {
		t.Fatalf("duplicate namespace leaves share wire name %q", document.Tools[0].Name)
	}
	wireItems := []json.RawMessage{json.RawMessage(`{"type":"function_call","call_id":"call_alpha","name":"` + document.Tools[0].Name + `","arguments":"{}"}`)}
	items, err := decodeCompletedResponsesItemSet(t.Context(), request, names, wireItems, "", "exchange", nil)
	if err != nil {
		t.Fatal(err)
	}
	call, ok := items[0].ToolCall()
	if !ok || call.Tool().Namespace() != "alpha" || call.Tool().Name() != "read_marker" {
		t.Fatalf("decoded call = %#v, want alpha/read_marker", items[0])
	}
}

func TestResponsesNormalFormLowersNamespaceToAttemptAliasAndNativeVisibility(t *testing.T) {
	request := responsesFunctionNamespaceRequest(t)
	declarations := canonicaltest.Tools(request)
	namespace, _ := declarations[0].Namespace()
	child := namespace.Tools()[0]
	set, _ := canonical.NewToolSet(declarations)
	refinements, err := canonical.NewToolVisibilityRefinements(set, []canonical.ToolKey{child.Key()})
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolDeclarationsItemWithVisibility(set, canonical.ContextScopeRequest, refinements)
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithItems([]canonical.CanonicalItem{item, canonicaltest.Message(t, canonical.MessageRoleUser, "Read the file")})
	request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: request.Items(),
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, pointerToToolKey(child.Key()))),
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	wireName, _ := names.WireName(child.Key())
	var changes []compat.Change
	document, err := LowerProviderRequestDocument(
		EncodeInput{Request: request, ToolNames: names}, delivery.BufferedDelivery(), &changes, "exchange",
		EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Tools) != 1 || document.Tools[0].Type != "function" || document.Tools[0].Name != wireName || document.Tools[0].DeferLoading == nil || !*document.Tools[0].DeferLoading {
		t.Fatalf("lowered Responses tools = %#v", document.Tools)
	}
	choice, ok := document.ToolChoice.(map[string]any)
	if !ok || choice["name"] != wireName {
		t.Fatalf("lowered Responses choice = %#v", document.ToolChoice)
	}
	if !hasResponseChange(changes, canonical.RequestTools) || hasResponseChange(changes, canonical.RequestToolsVisibility) {
		t.Fatalf("lowered Responses changes = %#v", changes)
	}
}

func pointerToToolKey(key canonical.ToolKey) *canonical.ToolKey { return &key }

func hasResponseChange(changes []compat.Change, capability canonical.CapabilityPath) bool {
	for _, change := range changes {
		if change.Capability == capability && change.Kind == compat.Approximation {
			return true
		}
	}
	return false
}

func responsesFunctionNamespaceRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	schema, err := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	childKey, err := canonical.NewToolKey("workspace", canonical.ToolKindFunction, "read_file")
	if err != nil {
		t.Fatal(err)
	}
	child := canonicaltest.MustFunctionTool(
		childKey,
		"Read a file",
		canonical.NewToolSchemaObject(schema),
		canonical.Unspecified[bool](),
	)
	namespace, err := canonical.NewToolNamespace(
		canonicaltest.MustRequestToolKey(canonical.ToolKindNamespace, "workspace"),
		"Workspace tools",
		[]canonical.ToolDeclaration{child},
	)
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, namespace),
			canonicaltest.Message(t, canonical.MessageRoleUser, "Read the file"),
		},
	})
}

func responsesDuplicateLeafNamespacesRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	schema, err := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	declarations := make([]canonical.ToolDeclaration, 0, 2)
	for _, namespaceName := range []string{"alpha", "beta"} {
		childKey, err := canonical.NewToolKey(namespaceName, canonical.ToolKindFunction, "read_marker")
		if err != nil {
			t.Fatal(err)
		}
		child := canonicaltest.MustFunctionTool(
			childKey,
			"Read the namespace marker",
			canonical.NewToolSchemaObject(schema),
			canonical.Unspecified[bool](),
		)
		namespace, err := canonical.NewToolNamespace(
			canonicaltest.MustRequestToolKey(canonical.ToolKindNamespace, namespaceName),
			namespaceName+" tools",
			[]canonical.ToolDeclaration{child},
		)
		if err != nil {
			t.Fatal(err)
		}
		declarations = append(declarations, namespace)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, declarations...),
			canonicaltest.Message(t, canonical.MessageRoleUser, "Read alpha marker"),
		},
	})
}

func decodeProviderTools(t *testing.T, raw []byte) []ProviderRequestTool {
	t.Helper()
	var payload struct {
		Tools []ProviderRequestTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Tools
}
