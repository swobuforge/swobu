package responses

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponsesToolPolicy_DefaultsBySurface(t *testing.T) {
	t.Parallel()

	plainFunctionTool := canonical.NewFunctionToolDecl(
		canonical.NewSemanticToolID("tool_0").String(),
		"grep",
		"search text",
		canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
	)
	namespacedFunctionTool := canonical.NewFunctionToolDecl(
		"workspace/grep",
		"grep",
		"search text",
		canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
	)
	plainCustomTool := canonical.NewCustomToolDecl(
		"apply_patch",
		"apply_patch",
		"edit files",
		canonical.NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`),
	)
	namespacedCustomTool := canonical.NewCustomToolDecl(
		"workspace/apply_patch",
		"apply_patch",
		"edit files",
		canonical.NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`),
	)
	projectedFunctionName, err := canonical.ProjectedToolName(namespacedFunctionTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(function) returned error: %v", err)
	}
	projectedCustomName, err := canonical.ProjectedToolName(namespacedCustomTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(custom) returned error: %v", err)
	}

	tests := []struct {
		name         string
		raw          string
		tools        []canonical.ToolDecl
		wantMode     canonical.ToolPolicyMode
		wantSpecific string
	}{
		{name: "empty without tools", raw: "", wantMode: canonical.ToolPolicyNone},
		{name: "null without tools", raw: "null", wantMode: canonical.ToolPolicyNone},
		{name: "empty with tools", raw: "", tools: []canonical.ToolDecl{plainFunctionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "null with tools", raw: "null", tools: []canonical.ToolDecl{plainFunctionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "string none", raw: `"none"`, tools: []canonical.ToolDecl{plainFunctionTool}, wantMode: canonical.ToolPolicyNone},
		{name: "string auto", raw: `"auto"`, tools: []canonical.ToolDecl{plainFunctionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "string required", raw: `"required"`, tools: []canonical.ToolDecl{plainFunctionTool}, wantMode: canonical.ToolPolicyRequired},
		{
			name:         "object plain function",
			raw:          `{"type":"function","name":"grep"}`,
			tools:        []canonical.ToolDecl{plainFunctionTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: plainFunctionTool.ToolID().String(),
		},
		{
			name:         "object projected function",
			raw:          `{"type":"function","name":"` + projectedFunctionName + `"}`,
			tools:        []canonical.ToolDecl{namespacedFunctionTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: namespacedFunctionTool.ToolID().String(),
		},
		{
			name:         "object plain custom",
			raw:          `{"type":"custom","name":"apply_patch"}`,
			tools:        []canonical.ToolDecl{plainCustomTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: plainCustomTool.ToolID().String(),
		},
		{
			name:         "object projected custom",
			raw:          `{"type":"custom","name":"` + projectedCustomName + `"}`,
			tools:        []canonical.ToolDecl{namespacedCustomTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: namespacedCustomTool.ToolID().String(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := DecodeResponsesToolPolicy(rawJSON(tc.raw), tc.tools)
			if err != nil {
				t.Fatalf("DecodeResponsesToolPolicy returned error: %v", err)
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
		})
	}
}

func TestDecodeResponsesToolPolicy_RejectsMalformedProjectedSpecificToolChoiceNameWithContext(t *testing.T) {
	t.Parallel()

	_, err := DecodeResponsesToolPolicy(rawJSON(`{"type":"function","name":"exec_command__bogus"}`), nil)
	if err == nil {
		t.Fatal("expected DecodeResponsesToolPolicy to reject a malformed projected tool_choice name")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
	if !strings.Contains(compatErr.Message, `responses request tool_choice.name (function) name "exec_command__bogus" is invalid`) {
		t.Fatalf("error message = %q, want tool_choice field context", compatErr.Message)
	}
	if !strings.Contains(compatErr.Message, "undeclared tool") {
		t.Fatalf("error message = %q, want undeclared tool cause", compatErr.Message)
	}
}

func TestDecodeResponsesToolPolicy_RejectsRawPlainNameForNamespacedTool(t *testing.T) {
	t.Parallel()

	namespacedFunctionTool := canonical.NewFunctionToolDecl(
		"workspace/grep",
		"grep",
		"search text",
		canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
	)

	_, err := DecodeResponsesToolPolicy(rawJSON(`{"type":"function","name":"grep"}`), []canonical.ToolDecl{namespacedFunctionTool})
	if err == nil {
		t.Fatal("expected DecodeResponsesToolPolicy to reject a raw plain tool_choice name for a namespaced tool")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
	if !strings.Contains(compatErr.Message, `responses request tool_choice.name (function) name "grep" is invalid`) {
		t.Fatalf("error message = %q, want tool_choice field context", compatErr.Message)
	}
	if !strings.Contains(compatErr.Message, "canonical request tool references are undeclared tool") {
		t.Fatalf("error message = %q, want undeclared tool cause", compatErr.Message)
	}
}

func TestEncodeToolChoice_WiresExplicitModes(t *testing.T) {
	t.Parallel()

	plainFunctionTool := canonical.NewFunctionToolDecl(
		canonical.NewSemanticToolID("tool_0").String(),
		"grep",
		"search text",
		canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
	)
	namespacedFunctionTool := canonical.NewFunctionToolDecl(
		"workspace/grep",
		"grep",
		"search text",
		canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
	)
	plainCustomTool := canonical.NewCustomToolDecl(
		"apply_patch",
		"apply_patch",
		"edit files",
		canonical.NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`),
	)
	namespacedCustomTool := canonical.NewCustomToolDecl(
		"workspace/apply_patch",
		"apply_patch",
		"edit files",
		canonical.NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`),
	)
	projectedFunctionName, err := canonical.ProjectedToolName(namespacedFunctionTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(function) returned error: %v", err)
	}
	projectedCustomName, err := canonical.ProjectedToolName(namespacedCustomTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(custom) returned error: %v", err)
	}

	tests := []struct {
		name   string
		policy canonical.ToolPolicy
		tools  []canonical.ToolDecl
		want   string
	}{
		{name: "none", policy: canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), tools: []canonical.ToolDecl{plainFunctionTool}, want: `"none"`},
		{name: "auto", policy: canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), tools: []canonical.ToolDecl{plainFunctionTool}, want: `"auto"`},
		{name: "required", policy: canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), tools: []canonical.ToolDecl{plainFunctionTool}, want: `"required"`},
		{
			name:   "specific plain function",
			policy: specificToolPolicy(plainFunctionTool.ToolID(), canonical.ToolTypeFunction),
			tools:  []canonical.ToolDecl{plainFunctionTool},
			want:   `{"type":"function","name":"grep"}`,
		},
		{
			name:   "specific projected function",
			policy: specificToolPolicy(namespacedFunctionTool.ToolID(), canonical.ToolTypeFunction),
			tools:  []canonical.ToolDecl{namespacedFunctionTool},
			want:   `{"type":"function","name":"` + projectedFunctionName + `"}`,
		},
		{
			name:   "specific plain custom",
			policy: specificToolPolicy(plainCustomTool.ToolID(), canonical.ToolTypeCustom),
			tools:  []canonical.ToolDecl{plainCustomTool},
			want:   `{"type":"custom","name":"apply_patch"}`,
		},
		{
			name:   "specific projected custom",
			policy: specificToolPolicy(namespacedCustomTool.ToolID(), canonical.ToolTypeCustom),
			tools:  []canonical.ToolDecl{namespacedCustomTool},
			want:   `{"type":"custom","name":"` + projectedCustomName + `"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := encodeToolChoice(tc.policy, tc.tools)
			if err != nil {
				t.Fatalf("encodeToolChoice returned error: %v", err)
			}
			assertJSONEqual(t, mustJSONString(t, got), tc.want)
		})
	}
}

func TestEncodeToolChoice_RejectsRequiredWithoutTools(t *testing.T) {
	t.Parallel()

	_, err := encodeToolChoice(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), nil)
	if err == nil {
		t.Fatal("expected encodeToolChoice to reject required without tools")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
}

func specificToolPolicy(id canonical.SemanticToolID, toolType string) canonical.ToolPolicy {
	policy := canonical.NewToolPolicy(canonical.ToolPolicySpecific, &id)
	policy.SpecificType = toolType
	return policy
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return string(raw)
}

func rawJSON(raw string) json.RawMessage {
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
}
