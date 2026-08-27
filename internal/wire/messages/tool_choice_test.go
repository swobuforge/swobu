package messages

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestClientRequestDecoder_DecodesToolChoiceBySurface(t *testing.T) {
	t.Parallel()

	functionToolDecl := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction,
		"codex/grep"),
		"search text", canonicaltest.Schema(t, `{"type":"object","properties":{"pattern":{"type":"string"}}}`), canonical.Unspecified[bool]())

	projectedFunctionName := functionToolDecl.Key().Name()
	functionTool := map[string]any{
		"name":         projectedFunctionName,
		"description":  "search text",
		"input_schema": map[string]any{"type": "object", "properties": map[string]any{"pattern": map[string]any{"type": "string"}}},
	}

	tests := []struct {
		name         string
		toolChoice   any
		includeTools bool
		wantMode     canonical.ToolPolicyMode
		wantSpecific string
	}{
		{name: "empty without tools", wantMode: canonical.ToolPolicyNone},
		{name: "empty with tools", includeTools: true, wantMode: canonical.ToolPolicyAuto},
		{name: "explicit none", toolChoice: map[string]any{"type": "none"}, includeTools: true, wantMode: canonical.ToolPolicyNone},
		{name: "explicit auto", toolChoice: map[string]any{"type": "auto"}, includeTools: true, wantMode: canonical.ToolPolicyAuto},
		{name: "explicit any", toolChoice: map[string]any{"type": "any"}, includeTools: true, wantMode: canonical.ToolPolicyRequired},
		{
			name:         "explicit tool",
			toolChoice:   map[string]any{"type": "tool", "name": projectedFunctionName},
			includeTools: true,
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, projectedFunctionName).String(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := messagesToolChoiceRequestJSON(t, tc.toolChoice, tc.includeTools, functionTool)
			got, resolvedDelivery, err := (testClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(
				protocolkind.Messages,
				"application/json",
				nil,
				raw,
				carrier.Meta{},
			))
			if err != nil {
				t.Fatalf("DecodeClientRequest returned error: %v", err)
			}
			if resolvedDelivery.Mode != delivery.Buffered {
				t.Fatalf("delivery mode = %s, want buffered", resolvedDelivery.Mode)
			}
			policy, err := got.EffectiveToolPolicy()
			if err != nil {
				t.Fatal(err)
			}
			if policy.Mode != tc.wantMode {
				t.Fatalf("effective tool policy mode = %q, want %q", policy.Mode, tc.wantMode)
			}
			if tc.toolChoice == nil && got.ToolPolicySpecified() {
				t.Fatal("omitted tool choice became a stored source fact")
			}
			if tc.wantSpecific == "" {
				return
			}
			specific, ok := policy.SpecificID()
			if !ok {
				t.Fatalf("tool policy specific is missing, want %q", tc.wantSpecific)
			}
			if specific.String() != tc.wantSpecific {
				t.Fatalf("tool policy specific = %q, want %q", specific, tc.wantSpecific)
			}
		})
	}
}

func TestEncodeCarrier_WiresToolChoiceAndRejectsUnsupportedRequired(t *testing.T) {
	t.Parallel()

	functionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction,
		"codex/grep"),
		"search text", canonicaltest.Schema(t, `{"type":"object","properties":{"pattern":{"type":"string"}}}`), canonical.Unspecified[bool]())

	baseRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("claude-haiku"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, functionTool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "hi"),
		},
	})
	projectedFunctionName, _ := testAttemptToolNames(baseRequest).WireName(functionTool.Key())

	tests := []struct {
		name     string
		policy   canonical.ToolPolicy
		tools    []canonical.ToolDeclaration
		wantJSON string
	}{
		{
			name:     "none",
			policy:   canonical.NewToolPolicy(canonical.ToolPolicyNone, nil),
			tools:    []canonical.ToolDeclaration{functionTool},
			wantJSON: `{"type":"none"}`,
		},
		{
			name:     "auto",
			policy:   canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil),
			tools:    []canonical.ToolDeclaration{functionTool},
			wantJSON: `{"type":"auto"}`,
		},
		{
			name:     "required",
			policy:   canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
			tools:    []canonical.ToolDeclaration{functionTool},
			wantJSON: `{"type":"any"}`,
		},
		{
			name: "specific",
			policy: specificToolPolicy(
				functionTool.Key(),
				canonical.ToolTypeFunction,
			),
			tools:    []canonical.ToolDeclaration{functionTool},
			wantJSON: `{"type":"tool","name":"` + projectedFunctionName + `"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model:      canonical.Specify(baseRequest.Model()),
				Items:      []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tc.tools...), canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
				ToolPolicy: canonical.Specify(tc.policy),
			})
			wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
			if err != nil {
				t.Fatalf("EncodeCarrier returned error: %v", err)
			}
			var payload struct {
				ToolChoice json.RawMessage `json:"tool_choice"`
				Tools      []any           `json:"tools"`
			}
			if err := json.Unmarshal(wire.Raw, &payload); err != nil {
				t.Fatalf("json.Unmarshal returned error: %v", err)
			}
			if len(payload.Tools) != 1 {
				t.Fatalf("tools len = %d, want 1", len(payload.Tools))
			}
			tool, ok := payload.Tools[0].(map[string]any)
			if !ok {
				t.Fatalf("tools[0] = %T, want map[string]any", payload.Tools[0])
			}
			if gotName, _ := tool["name"].(string); gotName != projectedFunctionName {
				t.Fatalf("tool name = %q, want %q", gotName, projectedFunctionName)
			}
			assertJSONEqual(t, string(payload.ToolChoice), tc.wantJSON)
		})
	}

	document, err := EncodeCarrier(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("claude-haiku"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "hi"),
		},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
	}), delivery.BufferedDelivery())
	if err != nil || len(document.RawBytes()) == 0 {
		t.Fatalf("document=%s err=%v", document.RawBytes(), err)
	}
}

func TestEncodeCarrier_OmitsToolChoiceWhenToolSurfaceIsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy canonical.ToolPolicy
	}{
		{
			name:   "none",
			policy: canonical.NewToolPolicy(canonical.ToolPolicyNone, nil),
		},
		{
			name:   "auto",
			policy: canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("claude-haiku"),
				Items: []canonical.CanonicalItem{
					canonicaltest.Message(t, canonical.MessageRoleUser, "hi"),
				},
				ToolPolicy: canonical.Specify(tc.policy),
			})
			wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
			if err != nil {
				t.Fatalf("EncodeCarrier returned error: %v", err)
			}
			var payload map[string]any
			if err := json.Unmarshal(wire.Raw, &payload); err != nil {
				t.Fatalf("json.Unmarshal returned error: %v", err)
			}
			if _, ok := payload["tool_choice"]; ok {
				t.Fatalf("tool_choice = %#v, want omitted on empty tool surface", payload["tool_choice"])
			}
		})
	}
}

func TestEncodeMessagesToolChoice_RecordsPolicyLossWithoutProjectedTools(t *testing.T) {
	t.Parallel()

	webSearch := canonical.NewWebSearchDeclaration()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, webSearch)}})
	_, lowered, err := compileMessagesTools([]canonical.ToolDeclaration{webSearch}, nil, testAttemptToolNames(request), nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	policies := []canonical.ToolPolicy{
		canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
		specificToolPolicy(webSearch.Key(), canonical.ToolTypeWebSearch),
	}
	for _, policy := range policies {
		policy := policy
		t.Run(string(policy.Mode), func(t *testing.T) {
			t.Parallel()
			var changes []compat.Change
			choice, err := encodeMessagesToolChoice(policy, lowered, nil, &changes, "")
			want := compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{})
			if err != nil || choice != nil || len(changes) != 1 || changes[0] != want {
				t.Fatalf("choice=%#v changes=%#v err=%v", choice, changes, err)
			}
		})
	}
}

func messagesToolChoiceRequestJSON(t *testing.T, toolChoice any, includeTools bool, tool map[string]any) []byte {
	t.Helper()
	payload := map[string]any{
		"model": "claude-haiku",
		"messages": []map[string]any{{
			"role":    "user",
			"content": "hi",
		}},
	}
	if toolChoice != nil {
		payload["tool_choice"] = toolChoice
	}
	if includeTools {
		payload["tools"] = []map[string]any{tool}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return raw
}

func specificToolPolicy(id canonical.ToolKey, toolType string) canonical.ToolPolicy {
	_ = toolType
	return canonical.NewToolPolicy(canonical.ToolPolicySpecific, &id)
}

func assertJSONEqual(t *testing.T, gotRaw, wantRaw string) {
	t.Helper()
	var got any
	var want any
	if err := json.Unmarshal([]byte(gotRaw), &got); err != nil {
		t.Fatalf("json.Unmarshal(got): %v", err)
	}
	if err := json.Unmarshal([]byte(wantRaw), &want); err != nil {
		t.Fatalf("json.Unmarshal(want): %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json mismatch\ngot:  %s\nwant: %s", gotRaw, wantRaw)
	}
}
