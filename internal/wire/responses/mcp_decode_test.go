package responses

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestResponsesTopLevelMCPSourceIsRequestScopedAndUsesServerDescription(t *testing.T) {
	raw := []byte(`{
		"model":"m","input":"test",
		"tools":[{
			"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc",
			"server_description":"Docs","allowed_tools":["search"],"require_approval":"never"
		}]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	declarations, ok := items[0].ToolDeclarations()
	if !ok || declarations.Scope() != canonical.ContextScopeRequest {
		t.Fatalf("MCP namespace = %#v", items)
	}
	sourceDeclaration := declarations.Tools().Declarations()[0]
	source, ok := sourceDeclaration.Namespace()
	remote, remoteOK := source.MCPSource()
	if !ok || !remoteOK {
		t.Fatalf("remote namespace = %#v", sourceDeclaration)
	}
	if source.Description() != "Docs" {
		t.Fatalf("server description = %q", source.Description())
	}
	if remote.Endpoint() != "https://mcp.example.test/rpc" {
		t.Fatalf("endpoint = %q", remote.Endpoint())
	}
}

func TestResponsesOrderedMCPSourceIsHistoryScoped(t *testing.T) {
	raw := []byte(`{"model":"m","input":[
		{"type":"additional_tools","role":"developer","tools":[
			{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"never"}
		]},
		{"type":"message","role":"user","content":"test"}
	]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	declarations, ok := decoded.Request.Request.Items()[0].ToolDeclarations()
	if !ok || declarations.Scope() != canonical.ContextScopeHistory {
		t.Fatalf("ordered MCP source = %#v", decoded.Request.Request.Items())
	}
}

func TestResponsesMCPSourcePreservesPositionAmongOrdinaryTools(t *testing.T) {
	raw := []byte(`{"model":"m","input":"test","tools":[
		{"type":"function","name":"before","parameters":{"type":"object"}},
		{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"never"},
		{"type":"function","name":"after","parameters":{"type":"object"}}
	]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 2 || items[0].Kind() != canonical.ItemKindToolDeclarations ||
		items[1].Kind() != canonical.ItemKindMessage {
		t.Fatalf("ordered MCP context = %#v", items)
	}
	declarations, _ := items[0].ToolDeclarations()
	ordered := declarations.Tools().Declarations()
	if len(ordered) != 3 || ordered[0].Key().Name() != "before" ||
		ordered[1].Key().Name() != "docs" || ordered[2].Key().Name() != "after" {
		t.Fatalf("ordered MCP declarations = %#v", ordered)
	}
}

func TestResponsesMCPUnsupportedSemanticsAreNotImplemented(t *testing.T) {
	for _, tool := range []string{
		`{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc"}`,
		`{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":null}`,
		`{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"always"}`,
		`{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"never","headers":{"Authorization":"Bearer secret"}}`,
		`{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"never","defer_loading":true}`,
		`{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"never","allowed_callers":["direct"]}`,
		`{"type":"mcp","server_label":"docs","connector_id":"connector_gmail","require_approval":"never"}`,
		`{"type":"mcp","server_label":"docs","tunnel_id":"tunnel_1","require_approval":"never"}`,
	} {
		raw := []byte(`{"model":"m","input":"test","tools":[` + tool + `]}`)
		_, err := (ClientRequestDecoder{}).DecodeClientRequest(
			carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
		)
		var canonicalError canonical.Error
		if !errors.As(err, &canonicalError) || canonicalError.Code != canonical.ErrorCodeNotImplemented {
			t.Fatalf("error = %T %v", err, err)
		}
	}
}

func TestResponsesMCPNullAndEmptyControlsAreOmitted(t *testing.T) {
	for _, controls := range []string{
		`"headers":null`,
		`"headers":{}`,
		`"headers":{ }`,
		`"allowed_callers":null`,
	} {
		raw := []byte(`{"model":"m","input":"test","tools":[{
			"type":"mcp","server_label":"docs",
			"server_url":"https://mcp.example.test/rpc",
			"require_approval":"never",` + controls + `}]}`)
		if _, err := (ClientRequestDecoder{}).DecodeClientRequest(
			carrier.NewDocument(
				protocolkind.Responses, "application/json", nil, raw, carrier.Meta{},
			),
		); err != nil {
			t.Fatalf("%s error = %T %v", controls, err, err)
		}
	}
}

func TestResponsesMCPAuthorizationIsDecodedWithItsDeclaration(t *testing.T) {
	raw := []byte(`{"model":"m","input":"test","tools":[{
		"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc",
		"require_approval":"never","authorization":"secret"
	}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindNamespace, "mcp/docs")
	if _, err := decoded.Request.MCPAccess.WithBearer(key, "different"); err == nil {
		t.Fatal("decoded MCP access did not retain the declaration's bearer")
	}
}

func TestResponsesMCPSourceSelectorsRequireExactlyOne(t *testing.T) {
	raw := []byte(`{"model":"m","input":"test","tools":[{
		"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc",
		"connector_id":"connector_gmail","require_approval":"never"
	}]}`)
	_, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	var canonicalError canonical.Error
	if !errors.As(err, &canonicalError) || canonicalError.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("selector error = %T %v", err, err)
	}
}

func TestResponsesExplicitEmptyInstructionsCreatesDirectiveOccurrence(t *testing.T) {
	raw := []byte(`{"model":"m","instructions":"","input":"test"}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 2 {
		t.Fatalf("items = %#v, want empty directive plus user message", items)
	}
	message, ok := items[0].Message()
	if !ok || message.Scope() != canonical.ContextScopeRequest {
		t.Fatalf("explicit empty directive = %#v", items[0])
	}
	content := message.Content()
	text, ok := content[0].Text()
	if !ok || text.Text() != "" {
		t.Fatalf("explicit empty instruction content = %#v", content)
	}
}

func TestResponsesMCPWithoutSourceSelectorIsBadRequest(t *testing.T) {
	raw := []byte(`{"model":"m","input":"test","tools":[{
		"type":"mcp","server_label":"docs","require_approval":"never"
	}]}`)
	_, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	var canonicalError canonical.Error
	if !errors.As(err, &canonicalError) || canonicalError.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("selector error = %T %v", err, err)
	}
}
