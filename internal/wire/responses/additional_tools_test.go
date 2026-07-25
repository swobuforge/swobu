package responses

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
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
	if !request.ToolsSpecified() || len(request.Tools()) != 1 {
		t.Fatalf("canonical tools = %#v, specified=%t", request.Tools(), request.ToolsSpecified())
	}
	if len(request.Items()) != 1 {
		t.Fatalf("canonical items = %#v, want only historical call", request.Items())
	}
	call, ok := request.Items()[0].ToolCall()
	if !ok || call.Tool() != request.Tools()[0].Key() {
		t.Fatalf("historical call = %#v, tools = %#v", call, request.Tools())
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
	if got := len(decoded.Request.Request.Tools()); got != 1 {
		t.Fatalf("canonical tool count = %d, want 1", got)
	}
}

func TestAdditionalToolsConflictingDualCarriersReturnBadRequest(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{
			"model":"m",
			"tools":[{"type":"function","name":"search","parameters":{"type":"object"}}],
			"input":[{"type":"additional_tools","role":"developer","tools":[
				{"type":"function","name":"inspect","parameters":{"type":"object"}}
			]}]
		}`),
		[]byte(`{
			"model":"m",
			"tools":[],
			"input":[{"type":"additional_tools","role":"developer","tools":[
				{"type":"function","name":"search","parameters":{"type":"object"}}
			]}]
		}`),
	} {
		assertAdditionalToolsErrorCode(t, raw, canonical.ErrorCodeBadRequest)
	}
}

func TestAdditionalToolsClassifyValidUnimplementedCarrierSemanticsAsSwobuOwned(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		code canonical.ErrorCode
	}{
		{
			name: "role",
			raw:  `{"input":[{"type":"additional_tools","role":"user","tools":[]}]}`,
			code: canonical.ErrorCodeNotImplemented,
		},
		{
			name: "position",
			raw:  `{"input":[{"type":"message","role":"user","content":"hi"},{"type":"additional_tools","role":"developer","tools":[]}]}`,
			code: canonical.ErrorCodeNotImplemented,
		},
		{
			name: "multiplicity",
			raw:  `{"input":[{"type":"additional_tools","role":"developer","tools":[]},{"type":"additional_tools","role":"developer","tools":[]}]}`,
			code: canonical.ErrorCodeNotImplemented,
		},
		{
			name: "unsupported tool kind",
			raw:  `{"input":[{"type":"additional_tools","role":"developer","tools":[{"type":"mcp","name":"remote"}]}]}`,
			code: canonical.ErrorCodeNotImplemented,
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

func TestAdditionalToolsUnsupportedKindReportsExactWireLocationAndType(t *testing.T) {
	raw := []byte(`{
		"input":[{"type":"additional_tools","role":"developer","tools":[
			{"type":"function","name":"search","parameters":{"type":"object"}},
			{"type":"namespace","name":"remote","tools":[
				{"type":"function","name":"read","parameters":{"type":"object"}},
				{"type":"mcp","name":"server"}
			]}
		]}]
	}`)
	_, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err == nil {
		t.Fatal("expected unsupported tool kind")
	}
	for _, detail := range []string{"/input/0/tools/1/tools/1/type", `"mcp"`} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("error %q does not identify %q", err, detail)
		}
	}
}

func TestAdditionalToolsPreambleSurvivesImplicitHistoryRebase(t *testing.T) {
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
	if rebased == nil || !rebased.Request.ToolsSpecified() || len(rebased.Request.Tools()) != 1 {
		t.Fatalf("rebased request lost invocation tools: %#v", rebased)
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
