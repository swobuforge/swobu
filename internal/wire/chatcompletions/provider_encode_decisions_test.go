package chatcompletions

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestProviderEncodeDecisionsDescribeActualChatCompletionsProjection(t *testing.T) {
	request := chatRequestWithStrictToolAndJSONSchema(t)
	result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestWithOptions(wire.ProviderEncodeInput{Request: request}, delivery.BufferedDelivery(), "exchange", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, feature := range []compat.Feature{compat.RequestToolsSchemaStrict, compat.RequestOutputFormat, compat.RequestOutputFormatSchema, compat.WireJSONMode} {
		assertChatDecision(t, result.Decisions, feature, compat.Exact)
	}
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
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, Tools: canonical.Specify(tools), OutputFormat: canonical.Specify(format)})
}

func assertChatDecision(t *testing.T, decisions []compat.Decision, feature compat.Feature, outcome compat.Outcome) {
	t.Helper()
	for _, decision := range decisions {
		if decision.Feature == feature && decision.Outcome == outcome {
			return
		}
	}
	t.Fatalf("decisions = %#v, want %s/%s", decisions, feature, outcome)
}
