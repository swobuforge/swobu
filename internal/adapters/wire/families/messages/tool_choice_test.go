package messages

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestClientRequestDecoder_DecodesToolChoiceBySurface(t *testing.T) {
	t.Parallel()

	functionToolDecl := canonical.NewFunctionToolDecl(
		"codex/grep",
		"grep",
		"search text",
		canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
	)
	projectedFunctionName, err := canonical.ProjectedToolName(functionToolDecl)
	if err != nil {
		t.Fatalf("ProjectedToolName returned error: %v", err)
	}
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
			wantSpecific: functionToolDecl.ToolID().String(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := messagesToolChoiceRequestJSON(t, tc.toolChoice, tc.includeTools, functionTool)
			got, resolvedDelivery, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.NewWireDocument(
				carrier.StageClientRequestIn,
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
			if got.ToolPolicy().Mode != tc.wantMode {
				t.Fatalf("tool policy mode = %q, want %q", got.ToolPolicy().Mode, tc.wantMode)
			}
			if tc.wantSpecific == "" {
				return
			}
			specific, ok := got.ToolPolicy().SpecificID()
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

	functionTool := canonical.NewFunctionToolDecl(
		"codex/grep",
		"grep",
		"search text",
		canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
	)
	projectedFunctionName, err := canonical.ProjectedToolName(functionTool)
	if err != nil {
		t.Fatalf("ProjectedToolName returned error: %v", err)
	}
	baseRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "claude-haiku",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
		Tools: []canonical.ToolDecl{functionTool},
	})

	tests := []struct {
		name     string
		policy   canonical.ToolPolicy
		tools    []canonical.ToolDecl
		wantJSON string
	}{
		{
			name:     "none",
			policy:   canonical.NewToolPolicy(canonical.ToolPolicyNone, nil),
			tools:    []canonical.ToolDecl{functionTool},
			wantJSON: `{"type":"none"}`,
		},
		{
			name:     "auto",
			policy:   canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil),
			tools:    []canonical.ToolDecl{functionTool},
			wantJSON: `{"type":"auto"}`,
		},
		{
			name:     "required",
			policy:   canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
			tools:    []canonical.ToolDecl{functionTool},
			wantJSON: `{"type":"any"}`,
		},
		{
			name: "specific",
			policy: specificToolPolicy(
				functionTool.ToolID(),
				canonical.ToolTypeFunction,
			),
			tools:    []canonical.ToolDecl{functionTool},
			wantJSON: `{"type":"tool","name":"` + projectedFunctionName + `"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model:      baseRequest.Model(),
				Items:      baseRequest.Items(),
				Tools:      tc.tools,
				ToolPolicy: tc.policy,
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

	_, err = EncodeCarrier(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "claude-haiku",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
		ToolPolicy: canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
	}), delivery.BufferedDelivery())
	if err == nil {
		t.Fatal("expected required tool choice without tools to be rejected")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
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

func specificToolPolicy(id canonical.SemanticToolID, toolType string) canonical.ToolPolicy {
	policy := canonical.NewToolPolicy(canonical.ToolPolicySpecific, &id)
	policy.SpecificType = toolType
	return policy
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
