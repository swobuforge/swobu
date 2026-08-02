package chatcompletions

import (
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
	refinements, _ := canonical.NewResponsesToolRefinements(set, []canonical.ToolKey{key})
	item, _ := canonical.NewToolDeclarationsItemWithResponses(set, canonical.ContextScopeRequest, refinements)
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item, canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}})
}

func chatRequestWithStrictToolAndJSONSchema(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	schema, err := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	tool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup"), "", canonical.NewToolSchemaObject(schema), canonical.Specify(true))
	tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{tool})
	if err != nil {
		t.Fatal(err)
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONSchema, Name: "answer", Schema: canonical.NewRawJSONObject(`{"type":"object"}`)})
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tools.Declarations()...), canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, OutputFormat: canonical.Specify(format)})
}
