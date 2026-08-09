package responses

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestDeferredFunctionRefinementRemainsNativeFromEveryRequestDeclarationCarrier(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "initial tools",
			raw:  `{"model":"m","input":"go","tools":[{"type":"function","name":"lookup","defer_loading":true,"parameters":{"type":"object"}}]}`,
		},
		{
			name: "nested namespace",
			raw:  `{"model":"m","input":"go","tools":[{"type":"namespace","name":"crm","tools":[{"type":"function","name":"lookup","defer_loading":true,"parameters":{"type":"object"}}]}]}`,
		},
		{
			name: "additional tools",
			raw:  `{"model":"m","input":[{"type":"additional_tools","role":"developer","tools":[{"type":"function","name":"lookup","defer_loading":true,"parameters":{"type":"object"}}]},{"type":"message","role":"user","content":"go"}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(
				protocolkind.Responses, "application/json", nil, []byte(test.raw), carrier.Meta{},
			))
			if err != nil {
				t.Fatal(err)
			}
			names, _, err := provider.BuildAttemptToolNames(decoded.Request.Request)
			if err != nil {
				t.Fatal(err)
			}
			var changes []compat.Change
			encoded, err := EncodeCarrierWithChanges(
				EncodeInput{Request: decoded.Request.Request, ToolNames: names}, delivery.BufferedDelivery(), &changes, "exchange", EncodeOptions{},
			)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(encoded.RawBytes()), `"defer_loading":true`) {
				t.Fatalf("native lowering omitted deferred visibility: %s", encoded.RawBytes())
			}
			if hasResponseChange(changes, canonical.RequestToolsVisibility) {
				t.Fatalf("native lowering reported visibility approximation: %#v", changes)
			}
		})
	}
}

func TestDeferredFunctionRefinementRemainsNativeFromDiscoveryResult(t *testing.T) {
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "loaded")
	schema, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	tool, _ := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(schema), canonical.Unspecified[bool]())
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{tool})
	refinements, _ := canonical.NewToolVisibilityRefinements(set, []canonical.ToolKey{key})
	callID, _ := canonical.NewToolCallID("search_1")
	result, err := canonical.NewToolDiscoveryResultItemWithVisibility(callID, set, canonical.DiscoveryExecutorClient, refinements)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result}})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	var changes []compat.Change
	encoded, err := EncodeCarrierWithChanges(EncodeInput{Request: request, ToolNames: names}, delivery.BufferedDelivery(), &changes, "exchange", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded.RawBytes()), `"defer_loading":true`) {
		t.Fatalf("native lowering omitted discovery-result deferred visibility: %s", encoded.RawBytes())
	}
	if hasResponseChange(changes, canonical.RequestToolsVisibility) {
		t.Fatalf("native lowering reported discovery-result visibility approximation: %#v", changes)
	}
}
