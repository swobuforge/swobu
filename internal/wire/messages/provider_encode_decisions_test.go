package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestProviderEncodeDecisionsDescribeActualMessagesProjection(t *testing.T) {
	request := requestWithStrictToolAndJSONSchema(t)
	result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: request}, delivery.BufferedDelivery(), "exchange")
	if err != nil {
		t.Fatal(err)
	}
	assertDecision(t, result.Changes, canonical.RequestToolsSchemaStrict, compat.Omission)
	assertDecision(t, result.Changes, canonical.RequestOutputFormat, compat.Approximation)
}

func requestWithStrictToolAndJSONSchema(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	schema, err := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	tool := canonicaltest.MustFunctionTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup"),
		"",
		canonical.NewToolSchemaObject(schema),
		canonical.Specify(true),
	)
	tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{tool})
	if err != nil {
		t.Fatal(err)
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONSchema, Name: "answer", Schema: canonical.NewRawJSONObject(`{"type":"object"}`)})
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Items:        []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tools.Declarations()...), canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		OutputFormat: canonical.Specify(format),
	})
}

func assertDecision(t *testing.T, changes []compat.Change, feature canonical.CapabilityPath, outcome compat.Kind) {
	t.Helper()
	for _, decision := range changes {
		if decision.Capability == feature && decision.Kind == outcome {
			return
		}
	}
	t.Fatalf("changes = %#v, want %s/%v", changes, feature, outcome)
}
