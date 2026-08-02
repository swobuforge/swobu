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

func TestOfficialResponsesLoweringFlattensFunctionNamespace(t *testing.T) {
	request := responsesFunctionNamespaceRequest(t)
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	child := canonicaltest.Tools(request)[0]
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

func TestResponsesNormalFormLowersNamespaceToAttemptAliasAndEagerVisibility(t *testing.T) {
	request := responsesFunctionNamespaceRequest(t)
	declarations := canonicaltest.Tools(request)
	namespace, _ := declarations[0].Namespace()
	child := namespace.Tools()[0]
	set, _ := canonical.NewToolSet(declarations)
	refinements, err := canonical.NewResponsesToolRefinements(set, []canonical.ToolKey{child.Key()})
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolDeclarationsItemWithResponses(set, canonical.ContextScopeRequest, refinements)
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
	if len(document.Tools) != 1 || document.Tools[0].Type != "function" || document.Tools[0].Name != wireName || document.Tools[0].DeferLoading != nil {
		t.Fatalf("lowered Responses tools = %#v", document.Tools)
	}
	choice, ok := document.ToolChoice.(map[string]any)
	if !ok || choice["name"] != wireName {
		t.Fatalf("lowered Responses choice = %#v", document.ToolChoice)
	}
	if !hasResponseChange(changes, canonical.RequestTools) || !hasResponseChange(changes, canonical.RequestToolsVisibility) {
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
