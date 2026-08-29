package messages

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestProviderEncodeDecisionsDescribeActualMessagesProjection(t *testing.T) {
	request := requestWithStrictToolAndJSONSchema(t)
	result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), "exchange")
	if err != nil {
		t.Fatal(err)
	}
	assertDecision(t, result.Changes, canonical.RequestToolsSchemaStrict, compat.Omission)
	assertDecision(t, result.Changes, canonical.RequestOutputFormat, compat.Approximation)
}

func TestNativeDeferredVisibilityEmitsNoApproximation(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	tool := canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	discoverySchema := canonicaltest.Schema(t, `{"type":"object","properties":{"pattern":{"type":"string"}}}`)
	discovery, err := canonical.NewToolDiscoveryToolWithQuery("find tools", discoverySchema, canonical.DiscoveryExecutorProvider, canonical.ToolDiscoveryQueryRegex)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, tool})
	refinements, _ := canonical.NewToolVisibilityRefinements(set, []canonical.ToolKey{key})
	item, _ := canonical.NewToolDeclarationsItemWithVisibility(set, canonical.ContextScopeRequest, refinements)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item, canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}})
	result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), "exchange")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range result.Changes {
		if change.Capability == canonical.RequestToolsVisibility && change.Kind == compat.Approximation {
			t.Fatalf("native deferred visibility reported approximation: %#v", result.Changes)
		}
	}
	if !strings.Contains(string(result.Document.RawBytes()), `"defer_loading":true`) {
		t.Fatalf("native Messages omitted defer_loading: %s", result.Document.RawBytes())
	}
}

func TestDeferredWebSearchRoundTripsNatively(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"user","content":"search"}],"tools":[{"type":"tool_search_tool_regex_20251119","name":"tool_search_tool_regex"},{"type":"web_search_20260209","name":"web_search","defer_loading":true}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(protocolkind.Messages, "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	rule := func(_ ToolLoweringContext, tool canonical.ToolDeclaration) ([]ProviderRequestTool, bool, []compat.Change, error) {
		if tool.Kind() != canonical.ToolKindWebSearch {
			return nil, false, nil, nil
		}
		return []ProviderRequestTool{{
			Type:           "web_search_20260209",
			Name:           canonical.WebSearchToolKey().Name(),
			AllowedCallers: []string{"direct"},
		}}, true, nil, nil
	}
	doc, err := CompileProviderRequestDocument(
		decoded.Request.Request,
		testAttemptToolNames(decoded.Request.Request),
		delivery.BufferedDelivery(),
		nil,
		"exchange",
		CompileOptions{LowerTool: rule},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EncodeProviderRequestDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.RawBytes()), `"type":"web_search_20260209"`) || !strings.Contains(string(result.RawBytes()), `"defer_loading":true`) {
		t.Fatalf("deferred web search did not round trip: %s", result.RawBytes())
	}
}

func TestMessagesApproximatesAllDeferredToolsWithOneEagerTool(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"user","content":"go"}],"tools":[{"name":"lookup","input_schema":{"type":"object"},"defer_loading":true}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(protocolkind.Messages, "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)}, delivery.BufferedDelivery(), "exchange")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.Document.RawBytes()), `"defer_loading":true`) {
		t.Fatalf("all-deferred visibility leaked unchanged: %s", result.Document.RawBytes())
	}
	want := compat.NewApproximation(canonical.RequestToolsVisibility, canonical.Occurrence{})
	if len(result.Changes) != 1 || result.Changes[0] != want {
		t.Fatalf("changes = %#v, want %#v", result.Changes, want)
	}
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
