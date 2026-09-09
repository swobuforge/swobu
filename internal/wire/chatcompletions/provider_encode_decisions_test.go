package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestExactProviderEncodeReturnsNoCompatibilityChanges(t *testing.T) {
	request := chatRequestWithStrictToolAndJSONSchema(t)
	result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), "exchange")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("changes = %#v, want exact-as-empty", result.Changes)
	}
}

func TestCustomOutputLoweringOwnsCompatibilityEvidence(t *testing.T) {
	request := chatRequestWithStrictToolAndJSONSchema(t)
	var changes []compat.Change
	custom := func(format canonical.OutputFormat, _ *[]compat.Change) (json.RawMessage, error) {
		strict := true
		return encodeChatCompletionsOutputFormat(format, &strict)
	}
	lowering := DefaultLowering()
	lowering.OutputFormat = custom
	_, err := CompileProviderRequestDocument(request, testAttemptToolNames(request), delivery.BufferedDelivery(), &changes, "exchange", CompileOptions{Lowering: lowering})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("outer compiler added output compatibility evidence: %#v", changes)
	}
}

func TestDeferredResponsesVisibilityIsEagerlyMaterializedOnce(t *testing.T) {
	request := deferredChatRequest(t)
	result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), "exchange")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, change := range result.Changes {
		if change.Capability == canonical.RequestToolsVisibility && change.Kind == compat.Approximation {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("visibility changes = %#v, want one request-wide approximation", result.Changes)
	}
}

func deferredChatRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	tool := canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{tool})
	refinements, _ := canonical.NewToolVisibilityRefinements(set, []canonical.ToolKey{key})
	item, _ := canonical.NewToolDeclarationsItemWithVisibility(set, canonical.ContextScopeRequest, refinements)
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item, canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}})
}

func chatRequestWithStrictToolAndJSONSchema(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	schema, err := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	tool, err := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(schema), canonical.SchemaContract{Profile: canonical.SchemaProfileOpenAI, Conformance: canonical.SchemaConformanceEnforced})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{tool})
	if err != nil {
		t.Fatal(err)
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONSchema, Name: "answer", Schema: canonical.NewRawJSONObject(`{"type":"object"}`), SchemaContract: canonical.SchemaContract{Profile: canonical.SchemaProfileOpenAI}})
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tools.Declarations()...), canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, OutputFormat: canonical.Specify(format)})
}
