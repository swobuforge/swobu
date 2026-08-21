package chatcompletions

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/testkit/providertest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestDecodeChatCompletionsToolChoice_DefaultsBySurface(t *testing.T) {
	t.Parallel()

	functionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction,
		"grep"),
		"search text", canonicaltest.Schema(t, `{"type":"object","properties":{"pattern":{"type":"string"}}}`), canonical.Unspecified[bool]())

	customTool := canonicaltest.MustCustomTool(canonicaltest.MustRequestToolKey(canonical.ToolKindCustom,
		"apply_patch"),

		"edit files",
		canonical.NewToolFormatObject(canonicaltest.Object(t, `{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`)))
	projectedFunctionName := providertest.ProjectedToolName(t, functionTool)
	projectedCustomName := providertest.ProjectedToolName(t, customTool)

	tests := []struct {
		name         string
		raw          string
		tools        []canonical.ToolDeclaration
		wantMode     canonical.ToolPolicyMode
		wantSpecific string
		wantType     string
	}{
		{name: "empty without tools", raw: "", wantMode: canonical.ToolPolicyNone},
		{name: "null without tools", raw: "null", wantMode: canonical.ToolPolicyNone},
		{name: "empty with tools", raw: "", tools: []canonical.ToolDeclaration{functionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "null with tools", raw: "null", tools: []canonical.ToolDeclaration{functionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "string none", raw: `"none"`, tools: []canonical.ToolDeclaration{functionTool}, wantMode: canonical.ToolPolicyNone},
		{name: "string auto", raw: `"auto"`, tools: []canonical.ToolDeclaration{functionTool}, wantMode: canonical.ToolPolicyAuto},
		{name: "string required", raw: `"required"`, tools: []canonical.ToolDeclaration{functionTool}, wantMode: canonical.ToolPolicyRequired},
		{
			name:         "object function",
			raw:          `{"type":"function","function":{"name":"` + projectedFunctionName + `"}}`,
			tools:        []canonical.ToolDeclaration{functionTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: functionTool.Key().String(),
			wantType:     canonical.ToolTypeFunction,
		},
		{
			name:         "object custom",
			raw:          `{"type":"custom","custom":{"name":"` + projectedCustomName + `"}}`,
			tools:        []canonical.ToolDeclaration{customTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: customTool.Key().String(),
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
				if specificType := string(specific.Kind()); specificType != tc.wantType {
					t.Fatalf("tool policy specific type = %q, want %q", specificType, tc.wantType)
				}
			}
		})
	}
}

func TestEncodeChatCompletionsToolChoice_WiresExplicitModes(t *testing.T) {
	t.Parallel()

	functionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction,
		"grep"),
		"search text", canonicaltest.Schema(t, `{"type":"object","properties":{"pattern":{"type":"string"}}}`), canonical.Unspecified[bool]())

	customTool := canonicaltest.MustCustomTool(canonicaltest.MustRequestToolKey(canonical.ToolKindCustom,
		"apply_patch"),

		"edit files",
		canonical.NewToolFormatObject(canonicaltest.Object(t, `{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`)))

	projectedFunctionName := providertest.ProjectedToolName(t, functionTool)
	projectedCustomName := providertest.ProjectedToolName(t, customTool)

	tests := []struct {
		name   string
		policy canonical.ToolPolicy
		tools  []canonical.ToolDeclaration
		want   string
	}{
		{name: "none", policy: canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), tools: []canonical.ToolDeclaration{functionTool}, want: `"none"`},
		{name: "auto", policy: canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), tools: []canonical.ToolDeclaration{functionTool}, want: `"auto"`},
		{name: "required", policy: canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), tools: []canonical.ToolDeclaration{functionTool}, want: `"required"`},
		{
			name:   "specific function",
			policy: specificToolPolicy(functionTool.Key(), canonical.ToolTypeFunction),
			tools:  []canonical.ToolDeclaration{functionTool},
			want:   `{"type":"function","function":{"name":"` + projectedFunctionName + `"}}`,
		},
		{
			name:   "specific custom",
			policy: specificToolPolicy(customTool.Key(), canonical.ToolTypeCustom),
			tools:  []canonical.ToolDeclaration{customTool},
			want:   `{"type":"custom","custom":{"name":"` + projectedCustomName + `"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var names provider.AttemptToolNames
			var lowered wire.LoweredToolSet
			if len(tc.tools) > 0 {
				set, _ := canonical.NewToolSet(tc.tools)
				item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
				names = testAttemptToolNames(canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}}))
				_, _, lowered, _ = compileChatCompletionsTools(tc.tools, names, nil, "", nil)
			}
			got, err := encodeChatCompletionsToolChoice(tc.policy, lowered, names, nil, "")
			if err != nil {
				t.Fatalf("encodeChatCompletionsToolChoice returned error: %v", err)
			}
			assertJSONEqual(t, mustJSONString(t, got), tc.want)
		})
	}
}

func TestEncodeChatCompletionsToolChoice_RejectsSemanticToolWithoutProviderPolicy(t *testing.T) {
	t.Parallel()
	webSearchTool := canonical.NewWebSearchDeclaration()
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{webSearchTool})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	names := testAttemptToolNames(canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}}))
	rule := func(_ ToolLoweringContext, tool canonical.ToolDeclaration) ([]any, bool, []compat.Change, error) {
		if tool.Kind() == canonical.ToolKindWebSearch {
			return []any{ProviderRequestTool{Type: "web_search"}}, true, nil, nil
		}
		return nil, false, nil, nil
	}
	_, _, lowered, err := compileChatCompletionsTools([]canonical.ToolDeclaration{webSearchTool}, names, nil, "", rule)
	if err != nil {
		t.Fatal(err)
	}
	policy := specificToolPolicy(webSearchTool.Key(), canonical.ToolTypeWebSearch)
	_, err = encodeChatCompletionsToolChoice(policy, lowered, names, nil, "")
	if err == nil {
		t.Fatal("expected encodeChatCompletionsToolChoice to reject semantic tool without provider policy")
	}
}

func TestEncodeChatCompletionsToolChoice_RejectsRequiredWithoutTools(t *testing.T) {
	t.Parallel()

	_, err := encodeChatCompletionsToolChoice(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), wire.LoweredToolSet{}, nil, nil, "")
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

			got, err := encodeChatCompletionsToolChoice(tc.policy, wire.LoweredToolSet{}, nil, nil, "")
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

func rawJSON(raw string) json.RawMessage {
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
}
