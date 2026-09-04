package responses

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
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
		t.Fatalf("MCP source = %#v", items)
	}
	sourceDeclaration := declarations.Tools().Declarations()[0]
	source, ok := sourceDeclaration.MCP()
	remote := source.Source()
	remoteOK := ok
	if !ok || !remoteOK {
		t.Fatalf("remote MCP source = %#v", sourceDeclaration)
	}
	if source.Description() != "Docs" {
		t.Fatalf("server description = %q", source.Description())
	}
	if endpoint, ok := remote.URL(); !ok || endpoint != "https://mcp.example.test/rpc" {
		t.Fatalf("endpoint = %q URL=%t", endpoint, ok)
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

func TestResponsesKnownMCPDeclarationsSurviveIngressTyped(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		kind     canonical.MCPSourceKind
		approval canonical.MCPApprovalKind
		loading  canonical.MCPLoading
		callers  bool
	}{
		{name: "missing approval defaults always", tool: `{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc"}`, kind: canonical.MCPSourceURL, approval: canonical.MCPApprovalAlways, loading: canonical.MCPLoadingEager},
		{name: "null approval defaults always", tool: `{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":null}`, kind: canonical.MCPSourceURL, approval: canonical.MCPApprovalAlways, loading: canonical.MCPLoadingEager},
		{name: "always", tool: `{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"always"}`, kind: canonical.MCPSourceURL, approval: canonical.MCPApprovalAlways, loading: canonical.MCPLoadingEager},
		{name: "headers", tool: `{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"never","headers":{"X-Tenant":"restricted"}}`, kind: canonical.MCPSourceURL, approval: canonical.MCPApprovalNever, loading: canonical.MCPLoadingEager},
		{name: "deferred", tool: `{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"never","defer_loading":true}`, kind: canonical.MCPSourceURL, approval: canonical.MCPApprovalNever, loading: canonical.MCPLoadingDeferred},
		{name: "callers", tool: `{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"never","allowed_callers":["direct"]}`, kind: canonical.MCPSourceURL, approval: canonical.MCPApprovalNever, loading: canonical.MCPLoadingEager, callers: true},
		{name: "connector", tool: `{"type":"mcp","server_label":"docs","connector_id":"connector_gmail","require_approval":"never"}`, kind: canonical.MCPSourceConnectorID, approval: canonical.MCPApprovalNever, loading: canonical.MCPLoadingEager},
		{name: "tunnel", tool: `{"type":"mcp","server_label":"docs","tunnel_id":"tunnel_1","require_approval":"never"}`, kind: canonical.MCPSourceTunnelID, approval: canonical.MCPApprovalNever, loading: canonical.MCPLoadingEager},
		{name: "filter", tool: `{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":{"always":{"tool_names":["write"]},"never":{"read_only":true}}}`, kind: canonical.MCPSourceURL, approval: canonical.MCPApprovalFilter, loading: canonical.MCPLoadingEager},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte(`{"model":"m","input":"test","tools":[
			{"type":"function","name":"safe_sibling","parameters":{"type":"object"}},` + test.tool + `]}`)
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
				carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
			)
			if err != nil {
				t.Fatal(err)
			}
			tools, err := canonical.EffectiveTools(decoded.Request.Request)
			if err != nil {
				t.Fatal(err)
			}
			declarations := tools.Declarations()
			if len(declarations) != 2 {
				t.Fatalf("declarations = %#v", declarations)
			}
			namespace, ok := declarations[1].MCP()
			source := namespace.Source()
			sourceOK := ok
			_, callersSet := source.AllowedCallers().Get()
			if !ok || !sourceOK || source.Kind() != test.kind ||
				source.Approval().Kind() != test.approval ||
				source.Loading() != test.loading || callersSet != test.callers {
				t.Fatalf("source = %#v", source)
			}
			if len(decoded.Changes) != 0 {
				t.Fatalf("ingress changes = %#v", decoded.Changes)
			}
		})
	}
}

func TestResponsesRequiredPolicySurvivesKnownMCPAdmission(t *testing.T) {
	raw := []byte(`{"model":"m","input":"test","tool_choice":"required","tools":[{
		"type":"mcp","server_label":"docs","connector_id":"connector_gmail",
		"require_approval":"never"
	}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatalf("wire decoder rejected MCP before residual validation: %v", err)
	}
	if _, err := continuity.Begin(decoded.Request.Request); err != nil {
		t.Fatalf("required MCP policy was invalidated at ingress: %v", err)
	}
}

func TestResponsesSpecificPolicySurvivesKnownMCPAdmission(t *testing.T) {
	raw := []byte(`{"model":"m","input":"test",
		"tool_choice":{"type":"mcp","server_label":"docs"},
		"tools":[{
			"type":"mcp","server_label":"docs","connector_id":"connector_gmail",
			"require_approval":"never"
		}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatalf("wire decoder rejected selected MCP before residual validation: %v", err)
	}
	if _, err := continuity.Begin(decoded.Request.Request); err != nil {
		t.Fatalf("specific MCP policy was invalidated at ingress: %v", err)
	}
}

func TestResponsesEmbeddedMCPFingerprintIncludesSemanticsButExcludesAccess(t *testing.T) {
	decode := func(tools string) wire.ClientRequestResult {
		t.Helper()
		raw := []byte(`{"model":"m","input":[
			{"type":"additional_tools","role":"developer","tools":[` + tools + `]},
			{"type":"message","role":"user","content":"test"}
		]}`)
		decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
			carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
		)
		if err != nil {
			t.Fatal(err)
		}
		return decoded.Request
	}
	first := decode(`
		{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc",
		 "require_approval":"never","headers":{"X-Tenant":"one"}}`)
	second := decode(`
		{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc",
		 "require_approval":"never","headers":{"X-Tenant":"two"}}`)
	differentSemantics := decode(`
		{"type":"mcp","server_label":"docs","connector_id":"connector_gmail",
		 "require_approval":"never","headers":{"X-Tenant":"one"}}`)
	if first.RequestFingerprint != second.RequestFingerprint {
		t.Fatal("transient MCP headers changed request fingerprint")
	}
	if first.RequestFingerprint == differentSemantics.RequestFingerprint {
		t.Fatal("typed MCP source semantics were absent from request fingerprint")
	}
}

func TestResponsesMalformedMCPStillFailsAtOwningOccurrence(t *testing.T) {
	for _, tool := range []string{
		`{"type":"mcp","server_url":"https://mcp.example.test/rpc","require_approval":"never"}`,
		`{"type":"mcp","server_label":"docs","server_url":"://bad","require_approval":"never"}`,
		`{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","connector_id":"also","require_approval":"never"}`,
		`{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":{}}`,
		`{"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc","require_approval":"never","headers":[]}`,
	} {
		raw := []byte(`{"model":"m","input":"test","tools":[` + tool + `]}`)
		_, err := (ClientRequestDecoder{}).DecodeClientRequest(
			carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
		)
		var canonicalError canonical.Error
		if !errors.As(err, &canonicalError) || canonicalError.Code != canonical.ErrorCodeBadRequest {
			t.Fatalf("malformed MCP error = %T %v, want BAD_REQUEST", err, err)
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
	key, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	if _, err := decoded.Request.MCPAccess.WithBearer(key, "different"); err == nil {
		t.Fatal("decoded MCP access did not retain the declaration's bearer")
	}
}

func TestResponsesMCPAuthorizationHeaderIsTransientAccess(t *testing.T) {
	raw := []byte(`{"model":"m","input":"test","tools":[{
		"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc",
		"require_approval":"never","headers":{"Authorization":"Bearer secret"}
	}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	if _, err := decoded.Request.MCPAccess.WithBearer(key, "different"); err == nil {
		t.Fatal("decoded MCP access did not retain Authorization header bearer")
	}
}

func TestResponsesMCPObjectAllowedToolsProjectsCanonicalSelection(t *testing.T) {
	raw := []byte(`{"model":"m","input":"test","tools":[{
		"type":"mcp","server_label":"docs","server_url":"https://mcp.example.test/rpc",
		"require_approval":"never","allowed_tools":{"tool_names":["search"]}
	}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	declarations, ok := items[0].ToolDeclarations()
	if !ok {
		t.Fatalf("items = %#v, want MCP declaration", items)
	}
	namespace, ok := declarations.Tools().Declarations()[0].MCP()
	if !ok {
		t.Fatal("MCP declaration did not preserve source authority")
	}
	source := namespace.Source()
	allowed, specified := source.AllowedTools().Get()
	if !specified || len(allowed) != 1 || allowed[0] != "search" {
		t.Fatalf("allowed tools = %#v specified=%t", allowed, specified)
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
