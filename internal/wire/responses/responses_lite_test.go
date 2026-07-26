package responses

import (
	"net/http"
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestCodexResponsesLiteSourceFixturesNormalizeOnlyVerifiedPrefix(t *testing.T) {
	for _, test := range []struct {
		name   string
		path   string
		header bool
	}{
		{name: "http marker", path: "testdata/codex_responses_lite_http.json", header: true},
		{name: "websocket metadata marker", path: "testdata/codex_responses_lite_websocket.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			headers := http.Header{}
			if test.header {
				headers.Set(responsesLiteHeader, "true")
			}
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
				carrier.NewDocument(protocolkind.Responses, "application/json", headers, raw, carrier.Meta{}),
			)
			if err != nil {
				t.Fatal(err)
			}
			items := decoded.Request.Request.Items()
			if len(items) != 3 {
				t.Fatalf("canonical item count = %d, want 3", len(items))
			}
			declarations, ok := items[0].ToolDeclarations()
			if !ok || declarations.Scope() != canonical.ContextScopeRequest {
				t.Fatalf("Lite declarations = %#v", items[0])
			}
			tools := declarations.Tools().Declarations()
			if len(tools) != 2 || tools[0].Kind() != canonical.ToolKindNamespace || tools[1].Kind() != canonical.ToolKindDiscovery {
				t.Fatalf("Lite tool tree = %#v", tools)
			}
			directive, ok := items[1].Message()
			if !ok || directive.Role() != canonical.MessageRoleDeveloper || directive.Scope() != canonical.ContextScopeRequest {
				t.Fatalf("Lite base instructions = %#v", items[1])
			}
		})
	}
}

func TestResponsesLiteMarkerIsRequiredForRequestScope(t *testing.T) {
	raw, err := os.ReadFile("testdata/codex_responses_lite_http.json")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 4 {
		t.Fatalf("ordinary additional_tools items = %#v", items)
	}
	directive, ok := items[0].Message()
	if !ok || directive.Scope() != canonical.ContextScopeRequest {
		t.Fatalf("explicit empty instructions were not retained: %#v", items[0])
	}
	declarations, ok := items[1].ToolDeclarations()
	if !ok || declarations.Scope() != canonical.ContextScopeHistory {
		t.Fatalf("ordinary additional_tools inferred Lite scope: %#v", items[1])
	}
}

func TestResponsesLiteMarkerFallsBackToOrdinaryResponsesSemantics(t *testing.T) {
	headers := http.Header{}
	headers.Set(responsesLiteHeader, "true")
	raw := []byte(`{
		"model":"m",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
		"input":"test"
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", headers, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 2 {
		t.Fatalf("ordinary marked request items = %#v", items)
	}
	declarations, ok := items[0].ToolDeclarations()
	if !ok || declarations.Scope() != canonical.ContextScopeRequest {
		t.Fatalf("ordinary top-level tools were not decoded normally: %#v", items[0])
	}
}
