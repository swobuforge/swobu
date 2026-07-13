package chatcompletions

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeChatCompletionsToolChoice_DefaultsBySurface(t *testing.T) {
	t.Parallel()

	functionTool := canonical.NewFunctionToolDecl(
		"codex/grep",
		"grep",
		"search text",
		canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
	)
	customTool := canonical.NewCustomToolDecl(
		"codex/apply_patch",
		"apply_patch",
		"edit files",
		canonical.NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`),
	)
	projectedFunctionName, err := canonical.ProjectedToolName(functionTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(function) returned error: %v", err)
	}
	projectedCustomName, err := canonical.ProjectedToolName(customTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(custom) returned error: %v", err)
	}

	tests := []struct {
		name         string
		raw          string
		tools        []canonical.ToolDecl
		wantMode     canonical.ToolPolicyMode
		wantSpecific string
		wantType     string
	}{
		{name: "empty without tools", raw: "", wantMode: canonical.ToolPolicyNone},
		{name: "null without tools", raw: "null", wantMode: canonical.ToolPolicyNone},
		{name: "empty with tools", raw: "", tools: []canonical.ToolDecl{functionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "null with tools", raw: "null", tools: []canonical.ToolDecl{functionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "string none", raw: `"none"`, tools: []canonical.ToolDecl{functionTool}, wantMode: canonical.ToolPolicyNone},
		{name: "string auto", raw: `"auto"`, tools: []canonical.ToolDecl{functionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "string required", raw: `"required"`, tools: []canonical.ToolDecl{functionTool}, wantMode: canonical.ToolPolicyRequired},
		{
			name:         "object function",
			raw:          `{"type":"function","function":{"name":"` + projectedFunctionName + `"}}`,
			tools:        []canonical.ToolDecl{functionTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: functionTool.ToolID().String(),
			wantType:     canonical.ToolTypeFunction,
		},
		{
			name:         "object custom",
			raw:          `{"type":"custom","custom":{"name":"` + projectedCustomName + `"}}`,
			tools:        []canonical.ToolDecl{customTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: customTool.ToolID().String(),
			wantType:     canonical.ToolTypeCustom,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeChatCompletionsToolChoice(rawJSON(tc.raw), tc.tools, nil, "")
			if err != nil {
				t.Fatalf("decodeChatCompletionsToolChoice returned error: %v", err)
			}
			if got.Mode != tc.wantMode {
				t.Fatalf("tool policy mode = %q, want %q", got.Mode, tc.wantMode)
			}
			if tc.wantSpecific == "" {
				if specific, ok := got.SpecificID(); ok {
					t.Fatalf("tool policy specific = %q, want none", specific)
				}
				return
			}
			specific, ok := got.SpecificID()
			if !ok {
				t.Fatalf("tool policy specific is missing, want %q", tc.wantSpecific)
			}
			if specific.String() != tc.wantSpecific {
				t.Fatalf("tool policy specific = %q, want %q", specific, tc.wantSpecific)
			}
			if tc.wantType != "" {
				if specificType, ok := got.SpecificToolType(); !ok || specificType != tc.wantType {
					t.Fatalf("tool policy specific type = %q, want %q", specificType, tc.wantType)
				}
			}
		})
	}
}

func TestEncodeChatCompletionsToolChoice_WiresExplicitModes(t *testing.T) {
	t.Parallel()

	functionTool := canonical.NewFunctionToolDecl(
		"codex/grep",
		"grep",
		"search text",
		canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
	)
	customTool := canonical.NewCustomToolDecl(
		"codex/apply_patch",
		"apply_patch",
		"edit files",
		canonical.NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`),
	)
	projectedFunctionName, err := canonical.ProjectedToolName(functionTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(function) returned error: %v", err)
	}
	projectedCustomName, err := canonical.ProjectedToolName(customTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(custom) returned error: %v", err)
	}

	tests := []struct {
		name   string
		policy canonical.ToolPolicy
		tools  []canonical.ToolDecl
		want   string
	}{
		{name: "none", policy: canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), tools: []canonical.ToolDecl{functionTool}, want: `"none"`},
		{name: "auto", policy: canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), tools: []canonical.ToolDecl{functionTool}, want: `"auto"`},
		{name: "required", policy: canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), tools: []canonical.ToolDecl{functionTool}, want: `"required"`},
		{
			name:   "specific function",
			policy: specificToolPolicy(functionTool.ToolID(), canonical.ToolTypeFunction),
			tools:  []canonical.ToolDecl{functionTool},
			want:   `{"type":"function","function":{"name":"` + projectedFunctionName + `"}}`,
		},
		{
			name:   "specific custom",
			policy: specificToolPolicy(customTool.ToolID(), canonical.ToolTypeCustom),
			tools:  []canonical.ToolDecl{customTool},
			want:   `{"type":"custom","custom":{"name":"` + projectedCustomName + `"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := encodeChatCompletionsToolChoice(tc.policy, tc.tools, nil, "")
			if err != nil {
				t.Fatalf("encodeChatCompletionsToolChoice returned error: %v", err)
			}
			assertJSONEqual(t, mustJSONString(t, got), tc.want)
		})
	}
}

func TestEncodeChatCompletionsToolChoice_RejectsRequiredWithoutTools(t *testing.T) {
	t.Parallel()

	_, err := encodeChatCompletionsToolChoice(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), nil, nil, "")
	if err == nil {
		t.Fatal("expected encodeChatCompletionsToolChoice to reject required without tools")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
}

func TestEncodeChatCompletionsToolChoice_OmitsEmptySurfaceChoices(t *testing.T) {
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

			got, err := encodeChatCompletionsToolChoice(tc.policy, nil, nil, "")
			if err != nil {
				t.Fatalf("encodeChatCompletionsToolChoice returned error: %v", err)
			}
			if got != nil {
				t.Fatalf("tool_choice = %#v, want omitted on empty tool surface", got)
			}
		})
	}
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return string(raw)
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

func rawJSON(raw string) json.RawMessage {
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
}
