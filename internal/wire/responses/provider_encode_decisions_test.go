package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestCustomOutputLoweringOwnsCompatibilityEvidence(t *testing.T) {
	request := responsesRequestWithStrictToolAndJSONSchema(t)
	var changes []compat.Change
	custom := func(format canonical.OutputFormat, _ *[]compat.Change) (*responsesTextDTO, error) {
		strict := true
		return encodeResponsesOutputFormat(format, &strict)
	}
	_, err := CompileProviderRequestDocument(
		EncodeInput{Request: request, ToolNames: testAttemptToolNames(request)},
		delivery.BufferedDelivery(), &changes, "exchange", EncodeOptions{},
		CompileOptions{ToolLowering: DefaultToolLowering(), OutputFormatLowering: custom},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("outer compiler added output compatibility evidence: %#v", changes)
	}
}

func TestExactProviderEncodeReturnsNoCompatibilityChanges(t *testing.T) {
	request := responsesRequestWithStrictToolAndJSONSchema(t)
	result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), "exchange")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("changes = %#v, want exact-as-empty", result.Changes)
	}
}

func responsesRequestWithStrictToolAndJSONSchema(t *testing.T) canonical.CanonicalRequest {
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
