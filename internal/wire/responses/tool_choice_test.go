package responses

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeResponsesToolPolicy_DefaultsBySurface(t *testing.T) {
	t.Parallel()

	plainFunctionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction,
		"grep"),
		"search text", canonicaltest.Schema(t, `{"type":"object","properties":{"pattern":{"type":"string"}}}`), canonical.Unspecified[bool]())

	namespacedFunctionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction,
		"grep2"),
		"search text", canonicaltest.Schema(t, `{"type":"object","properties":{"pattern":{"type":"string"}}}`), canonical.Unspecified[bool]())

	plainCustomTool := canonicaltest.MustCustomTool(canonicaltest.MustRequestToolKey(canonical.ToolKindCustom,
		"apply_patch"),

		"edit files",
		canonical.NewToolFormatObject(canonicaltest.Object(t, `{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`)))

	namespacedCustomTool := canonicaltest.MustCustomTool(canonicaltest.MustRequestToolKey(canonical.ToolKindCustom,
		"apply_patch2"),

		"edit files",
		canonical.NewToolFormatObject(canonicaltest.Object(t, `{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`)))

	functionNames := toolChoiceNames(t, []canonical.ToolDeclaration{namespacedFunctionTool})
	customNames := toolChoiceNames(t, []canonical.ToolDeclaration{namespacedCustomTool})
	projectedFunctionName, _ := functionNames.WireName(namespacedFunctionTool.Key())
	projectedCustomName, _ := customNames.WireName(namespacedCustomTool.Key())
	webSearchTool := canonical.NewWebSearchDeclaration()

	tests := []struct {
		name         string
		raw          string
		tools        []canonical.ToolDeclaration
		wantMode     canonical.ToolPolicyMode
		wantSpecific string
	}{
		{name: "empty without tools", raw: "", wantMode: canonical.ToolPolicyNone},
		{name: "null without tools", raw: "null", wantMode: canonical.ToolPolicyNone},
		{name: "empty with tools", raw: "", tools: []canonical.ToolDeclaration{plainFunctionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "null with tools", raw: "null", tools: []canonical.ToolDeclaration{plainFunctionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "string none", raw: `"none"`, tools: []canonical.ToolDeclaration{plainFunctionTool}, wantMode: canonical.ToolPolicyNone},
		{name: "string auto", raw: `"auto"`, tools: []canonical.ToolDeclaration{plainFunctionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "string required", raw: `"required"`, tools: []canonical.ToolDeclaration{plainFunctionTool}, wantMode: canonical.ToolPolicyRequired},
		{
			name:         "object web search",
			raw:          `{"type":"web_search"}`,
			tools:        []canonical.ToolDeclaration{webSearchTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: webSearchTool.Key().String(),
		},
		{
			name:         "object plain function",
			raw:          `{"type":"function","name":"grep"}`,
			tools:        []canonical.ToolDeclaration{plainFunctionTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: plainFunctionTool.Key().String(),
		},
		{
			name:         "object projected function",
			raw:          `{"type":"function","name":"` + projectedFunctionName + `"}`,
			tools:        []canonical.ToolDeclaration{namespacedFunctionTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: namespacedFunctionTool.Key().String(),
		},
		{
			name:         "object plain custom",
			raw:          `{"type":"custom","name":"apply_patch"}`,
			tools:        []canonical.ToolDeclaration{plainCustomTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: plainCustomTool.Key().String(),
		},
		{
			name:         "object projected custom",
			raw:          `{"type":"custom","name":"` + projectedCustomName + `"}`,
			tools:        []canonical.ToolDeclaration{namespacedCustomTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: namespacedCustomTool.Key().String(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := DecodeResponsesToolPolicy(rawJSON(tc.raw), tc.tools, nil, "")
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

	_, err := DecodeResponsesToolPolicy(rawJSON(`{"type":"function","name":"exec_command__bogus"}`), nil, nil, "")
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

	namespacedFunctionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction,
		"grep2"),
		"search text", canonicaltest.Schema(t, `{"type":"object","properties":{"pattern":{"type":"string"}}}`), canonical.Unspecified[bool]())

	_, err := DecodeResponsesToolPolicy(rawJSON(`{"type":"function","name":"grep"}`), []canonical.ToolDeclaration{namespacedFunctionTool}, nil, "")
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

	plainFunctionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction,
		"grep"),
		"search text", canonicaltest.Schema(t, `{"type":"object","properties":{"pattern":{"type":"string"}}}`), canonical.Unspecified[bool]())

	namespacedFunctionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction,
		"workspace/grep"),
		"search text", canonicaltest.Schema(t, `{"type":"object","properties":{"pattern":{"type":"string"}}}`), canonical.Unspecified[bool]())

	plainCustomTool := canonicaltest.MustCustomTool(canonicaltest.MustRequestToolKey(canonical.ToolKindCustom,
		"apply_patch"),

		"edit files",
		canonical.NewToolFormatObject(canonicaltest.Object(t, `{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`)))

	namespacedCustomTool := canonicaltest.MustCustomTool(canonicaltest.MustRequestToolKey(canonical.ToolKindCustom,
		"apply_patch2"),

		"edit files",
		canonical.NewToolFormatObject(canonicaltest.Object(t, `{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`)))

	functionNames := toolChoiceNames(t, []canonical.ToolDeclaration{namespacedFunctionTool})
	projectedFunctionName, _ := functionNames.WireName(namespacedFunctionTool.Key())
	customNames := toolChoiceNames(t, []canonical.ToolDeclaration{namespacedCustomTool})
	projectedCustomName, _ := customNames.WireName(namespacedCustomTool.Key())

	tests := []struct {
		name   string
		policy canonical.ToolPolicy
		tools  []canonical.ToolDeclaration
		want   string
	}{
		{name: "none without tools is inert", policy: canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), want: `null`},
		{name: "none", policy: canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), tools: []canonical.ToolDeclaration{plainFunctionTool}, want: `"none"`},
		{name: "auto", policy: canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), tools: []canonical.ToolDeclaration{plainFunctionTool}, want: `"auto"`},
		{name: "required", policy: canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), tools: []canonical.ToolDeclaration{plainFunctionTool}, want: `"required"`},
		{
			name:   "specific plain function",
			policy: specificToolPolicy(plainFunctionTool.Key(), canonical.ToolTypeFunction),
			tools:  []canonical.ToolDeclaration{plainFunctionTool},
			want:   `{"type":"function","name":"grep"}`,
		},
		{
			name:   "specific projected function",
			policy: specificToolPolicy(namespacedFunctionTool.Key(), canonical.ToolTypeFunction),
			tools:  []canonical.ToolDeclaration{namespacedFunctionTool},
			want:   `{"type":"function","name":"` + projectedFunctionName + `"}`,
		},
		{
			name:   "specific plain custom",
			policy: specificToolPolicy(plainCustomTool.Key(), canonical.ToolTypeCustom),
			tools:  []canonical.ToolDeclaration{plainCustomTool},
			want:   `{"type":"custom","name":"apply_patch"}`,
		},
		{
			name:   "specific projected custom",
			policy: specificToolPolicy(namespacedCustomTool.Key(), canonical.ToolTypeCustom),
			tools:  []canonical.ToolDeclaration{namespacedCustomTool},
			want:   `{"type":"custom","name":"` + projectedCustomName + `"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names := toolChoiceNames(t, tc.tools)
			projection := responsesToolProjection{emitted: make(map[canonical.ToolKey][]ProviderRequestTool)}
			if len(tc.tools) > 0 {
				projection, _ = compileResponsesToolProjection(tc.tools, canonical.ToolVisibilityRefinements{}, names, nil, "", nil)
			}
			got, err := encodeToolChoice(tc.policy, projection, names, nil, "")
			if err != nil {
				t.Fatalf("encodeToolChoice returned error: %v", err)
			}
			assertJSONEqual(t, mustJSONString(t, got), tc.want)
		})
	}
}

func TestEncodeToolChoice_RecordsPolicyLossWithoutProjectedTools(t *testing.T) {
	t.Parallel()

	webSearch := canonical.NewWebSearchDeclaration()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, webSearch)}})
	projection, err := compileResponsesToolProjection([]canonical.ToolDeclaration{webSearch}, canonical.ToolVisibilityRefinements{}, testAttemptToolNames(request), nil, "", nil)
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
			choice, err := encodeToolChoice(policy, projection, nil, &changes, "")
			want := compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{})
			if err != nil || choice != nil || len(changes) != 1 || changes[0] != want {
				t.Fatalf("choice=%#v changes=%#v err=%v", choice, changes, err)
			}
		})
	}
}

func toolChoiceNames(t *testing.T, tools []canonical.ToolDeclaration) provider.AttemptToolNames {
	t.Helper()
	if len(tools) == 0 {
		return provider.AttemptToolNames{}
	}
	set, err := canonical.NewToolSet(tools)
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	names, _, err := provider.BuildAttemptToolNames(canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}}))
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func specificToolPolicy(id canonical.ToolKey, toolType string) canonical.ToolPolicy {
	_ = toolType
	return canonical.NewToolPolicy(canonical.ToolPolicySpecific, &id)
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
