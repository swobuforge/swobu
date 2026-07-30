package responses

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestAdditionalToolsLiftIntoCanonicalAndResolveHistoricalCalls(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"input":[
			{"type":"additional_tools","role":"developer","id":"presentation-only","future":true,"tools":[
				{"type":"function","name":"search","description":"Search docs","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}
			]},
			{"type":"function_call","call_id":"call_1","name":"search","arguments":"{\"query\":\"deadline\"}"}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := decoded.Request.Request
	if len(canonicaltest.Tools(request)) != 1 {
		t.Fatalf("canonical tools = %#v", canonicaltest.Tools(request))
	}
	if len(request.Items()) != 2 {
		t.Fatalf("canonical items = %#v, want declaration and historical call", request.Items())
	}
	declarations, declared := request.Items()[0].ToolDeclarations()
	if !declared || declarations.Scope() != canonical.ContextScopeHistory {
		t.Fatalf("ordinary additional_tools = %#v, want ordered history declaration", request.Items()[0])
	}
	call, ok := request.Items()[1].ToolCall()
	if !ok || call.Tool() != canonicaltest.Tools(request)[0].Key() {
		t.Fatalf("historical call = %#v, tools = %#v", call, canonicaltest.Tools(request))
	}
}

func TestAdditionalToolsIdenticalDualCarriersReconcileOnce(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"tools":[{"strict":true,"parameters":{"required":["query"],"properties":{"query":{"type":"string"}},"type":"object"},"name":"search","type":"function","description":"Search docs"}],
		"input":[
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"function","name":"search","description":" Search docs ","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]},"strict":true}
			]},
			{"type":"message","role":"user","content":"search"}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(canonicaltest.Tools(decoded.Request.Request)); got != 1 {
		t.Fatalf("canonical tool count = %d, want 1", got)
	}
}

func TestAdditionalToolsConflictingRedeclarationReturnsBadRequest(t *testing.T) {
	raw := []byte(`{
			"model":"m",
			"tools":[{"type":"function","name":"search","parameters":{"type":"object"}}],
			"input":[{"type":"additional_tools","role":"developer","tools":[
				{"type":"function","name":"search","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}
			]}]
		}`)
	assertAdditionalToolsErrorCode(t, raw, canonical.ErrorCodeBadRequest)
}

func TestAdditionalToolsMalformedKnownSemanticsRemainBadRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		code canonical.ErrorCode
	}{
		{
			name: "malformed MCP source",
			raw:  `{"input":[{"type":"additional_tools","role":"developer","tools":[{"type":"mcp","name":"remote"}]}]}`,
			code: canonical.ErrorCodeBadRequest,
		},
		{
			name: "malformed tools",
			raw:  `{"input":[{"type":"additional_tools","role":"developer","tools":"invalid"}]}`,
			code: canonical.ErrorCodeBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertAdditionalToolsErrorCode(t, []byte(tc.raw), tc.code)
		})
	}
}

func TestAdditionalToolsErasesSemanticallyEmptyOccurrences(t *testing.T) {
	raw := []byte(`{"input":[
		{"type":"message","role":"user","content":"hi"},
		{"type":"additional_tools","role":"developer","tools":[]},
		{"type":"additional_tools","role":"developer","tools":[]}
	]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindMessage {
		t.Fatalf("empty additional_tools changed canonical items: %#v", items)
	}
}

func TestAdditionalToolsUnfamiliarCarrierRoleDoesNotReject(t *testing.T) {
	raw := []byte(`{"input":[
		{"type":"additional_tools","role":"future_directive","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}]},
		{"type":"message","role":"user","content":"search"}
	]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(canonicaltest.Tools(decoded.Request.Request)); got != 1 {
		t.Fatalf("canonical tool count = %d, want 1", got)
	}
	items := decoded.Request.Request.Items()
	if len(items) < 1 {
		t.Fatal("canonical request has no declaration occurrence")
	}
	declarations, ok := items[0].ToolDeclarations()
	if !ok || declarations.Scope() != canonical.ContextScopeHistory {
		t.Fatalf("additional_tools occurrence = %#v, want history scope", items[0])
	}
	if len(decoded.Changes) != 1 {
		t.Fatalf("compatibility changes = %#v, want role approximation evidence", decoded.Changes)
	}
	item, occurrenceOK := decoded.Changes[0].Occurrence.RequestItem()
	if decoded.Changes[0].Capability != canonical.RequestItemsMessageRole || decoded.Changes[0].Kind != compat.Approximation || !occurrenceOK || item != 0 {
		t.Fatalf("compatibility changes = %#v, want role approximation evidence", decoded.Changes)
	}
}

func TestAdditionalToolsPreambleRemainsWithPriorHistoryAfterImplicitRebase(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"input":[
			{"type":"additional_tools","role":"developer","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}]},
			{"type":"message","role":"user","content":"turn one"},
			{"type":"message","role":"assistant","status":"completed","content":"answer"},
			{"type":"message","role":"user","content":"turn two"}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	rebased := decoded.Request.RebasedRequest
	if rebased == nil || len(canonicaltest.Tools(rebased.Request)) != 0 {
		t.Fatalf("rebased request repeated historical declarations: %#v", rebased)
	}
	if len(rebased.Request.Items()) != 1 {
		t.Fatalf("rebased items = %#v, want current message", rebased.Request.Items())
	}
}

func TestAdditionalToolsLexicalDifferencesDoNotChangeRequestIdentity(t *testing.T) {
	decode := func(tool string) historyfingerprint.Request {
		t.Helper()
		raw := []byte(`{"model":"m","input":[{"type":"additional_tools","role":"developer","tools":[` + tool + `]},{"type":"message","role":"user","content":"hi"}]}`)
		decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
			carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
		)
		if err != nil {
			t.Fatal(err)
		}
		return decoded.Request.RequestFingerprint
	}
	left := decode(`{"type":"function","name":"search","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}`)
	right := decode(`{"parameters":{"properties":{"q":{"type":"string"}},"type":"object"},"name":"search","type":"function"}`)
	if left != right {
		t.Fatalf("lexically equivalent carriers changed request identity: %q != %q", left, right)
	}
}

func assertAdditionalToolsErrorCode(t *testing.T, raw []byte, want canonical.ErrorCode) {
	t.Helper()
	_, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err == nil {
		t.Fatalf("expected %s", want)
	}
	var canonicalError canonical.Error
	if !errors.As(err, &canonicalError) || canonicalError.Code != want {
		t.Fatalf("error = %T %v, want %s", err, err, want)
	}
	if strings.Contains(err.Error(), "presentation-only") {
		t.Fatalf("error exposed item metadata: %v", err)
	}
}
