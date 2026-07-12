package canonical

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestToolPolicyClone_PreservesSpecificToolID(t *testing.T) {
	t.Parallel()

	id := NewSemanticToolID("tool_0")
	policy := NewToolPolicy(ToolPolicySpecific, &id)
	policy.SpecificType = "custom"

	cloned := policy.Clone()
	specific, ok := cloned.SpecificID()
	if !ok {
		t.Fatal("cloned tool policy lost specific tool id")
	}
	if specific.String() != "tool_0" {
		t.Fatalf("specific tool id = %q, want %q", specific, "tool_0")
	}
	if cloned.Mode != ToolPolicySpecific {
		t.Fatalf("tool policy mode = %q, want %q", cloned.Mode, ToolPolicySpecific)
	}
	if specificType, ok := cloned.SpecificToolType(); !ok || specificType != "custom" {
		t.Fatalf("tool policy specific type = %q, want %q", specificType, "custom")
	}
}

func TestSemanticToolID_RoundTripsCanonicalString(t *testing.T) {
	t.Parallel()

	id := NewSemanticToolIDFor(ToolOrigin("provider"), ToolKindCapability, "web_search")
	if got, want := id.String(), "tool:v1/provider/capability/web_search"; got != want {
		t.Fatalf("canonical id string = %q, want %q", got, want)
	}
	parsed := NewSemanticToolID(id.String())
	if parsed != id {
		t.Fatalf("parsed id = %#v, want %#v", parsed, id)
	}
}

func TestRequestToolDeclMetadata_RoundTripsCapabilityDeclarations(t *testing.T) {
	t.Parallel()

	decl := CapabilityToolDecl{
		ID:         NewSemanticToolID("cap_0"),
		Capability: NewToolCapability("web_search"),
		Config:     NewToolCapabilityConfigObject(`{"region":"us"}`),
		Execution:  ToolOwnerProvider,
	}

	raw, err := encodeRequestToolDeclsMetadata([]ToolDecl{decl})
	if err != nil {
		t.Fatalf("encodeRequestToolDeclsMetadata returned error: %v", err)
	}
	decoded, err := decodeRequestToolDeclsMetadata(raw)
	if err != nil {
		t.Fatalf("decodeRequestToolDeclsMetadata returned error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded len = %d, want 1", len(decoded))
	}
	got, ok := decoded[0].(CapabilityToolDecl)
	if !ok {
		t.Fatalf("decoded declaration = %T, want CapabilityToolDecl", decoded[0])
	}
	if got.ToolID() != decl.ToolID() {
		t.Fatalf("tool id = %q, want %q", got.ToolID(), decl.ToolID())
	}
	if got.ToolCapability() != decl.ToolCapability() {
		t.Fatalf("capability = %q, want %q", got.ToolCapability(), decl.ToolCapability())
	}
	if got.Owner() != ToolOwnerProvider {
		t.Fatalf("owner = %q, want %q", got.Owner(), ToolOwnerProvider)
	}
	if got.CapabilityConfig().RawObject() != `{"region":"us"}` {
		t.Fatalf("capability config = %q, want %q", got.CapabilityConfig().RawObject(), `{"region":"us"}`)
	}
}

func TestRequestToolDeclMetadata_RoundTripsCustomAndFunctionDeclarations(t *testing.T) {
	t.Parallel()

	custom := CustomToolDecl{
		ID:          NewSemanticToolID("apply_patch"),
		Name:        "apply_patch",
		Description: "Use the apply_patch tool to edit files.",
		Format:      NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`),
		Execution:   ToolOwnerClient,
	}
	function := NewFunctionToolDecl(
		"codex/exec_command",
		"exec_command",
		"Run a shell command.",
		NewToolSchemaObject(`{"type":"object","properties":{"cmd":{"type":"string"}}}`),
	)

	raw, err := encodeRequestToolDeclsMetadata([]ToolDecl{custom, function})
	if err != nil {
		t.Fatalf("encodeRequestToolDeclsMetadata returned error: %v", err)
	}
	decoded, err := decodeRequestToolDeclsMetadata(raw)
	if err != nil {
		t.Fatalf("decodeRequestToolDeclsMetadata returned error: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded len = %d, want 2", len(decoded))
	}
	gotCustom, ok := decoded[0].(CustomToolDecl)
	if !ok {
		t.Fatalf("decoded declaration = %T, want CustomToolDecl", decoded[0])
	}
	if gotCustom.ToolName() != custom.ToolName() {
		t.Fatalf("custom tool name = %q, want %q", gotCustom.ToolName(), custom.ToolName())
	}
	if !jsonObjectsEqual(gotCustom.Format.RawObject(), custom.Format.RawObject()) {
		t.Fatalf("custom format = %q, want %q", gotCustom.Format.RawObject(), custom.Format.RawObject())
	}
	gotFunction, ok := decoded[1].(FunctionToolDecl)
	if !ok {
		t.Fatalf("decoded declaration = %T, want FunctionToolDecl", decoded[1])
	}
	if gotFunction.ToolName() != function.ToolName() {
		t.Fatalf("function tool name = %q, want %q", gotFunction.ToolName(), function.ToolName())
	}
	if gotFunction.ToolID() != function.ToolID() {
		t.Fatalf("function tool id = %q, want %q", gotFunction.ToolID(), function.ToolID())
	}
	if !jsonObjectsEqual(gotFunction.ToolInputSchema().RawObject(), function.ToolInputSchema().RawObject()) {
		t.Fatalf("function input schema = %q, want %q", gotFunction.ToolInputSchema().RawObject(), function.ToolInputSchema().RawObject())
	}
}

func TestProjectedToolName_RoundTripsStructuredFunctionAndCustomDeclarations(t *testing.T) {
	t.Parallel()

	function := NewFunctionToolDecl(
		"codex/exec_command",
		"exec_command",
		"Run a shell command.",
		NewToolSchemaObject(`{"type":"object","properties":{"cmd":{"type":"string"}}}`),
	)
	custom := NewCustomToolDecl(
		"codex/apply_patch",
		"apply_patch",
		"Use the apply_patch tool to edit files.",
		NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`),
	)

	tests := []struct {
		name     string
		tool     ToolDecl
		wantKind ToolKind
		wantName string
		wantID   SemanticToolID
	}{
		{
			name:     "function",
			tool:     function,
			wantKind: ToolKindFunction,
			wantName: "exec_command",
			wantID:   function.ToolID(),
		},
		{
			name:     "custom",
			tool:     custom,
			wantKind: ToolKindCustom,
			wantName: "apply_patch",
			wantID:   custom.ToolID(),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projected, err := ProjectedToolName(tc.tool)
			if err != nil {
				t.Fatalf("ProjectedToolName returned error: %v", err)
			}
			parsedID, leaf, err := ParseProjectedToolName(projected, tc.wantKind)
			if err != nil {
				t.Fatalf("ParseProjectedToolName returned error: %v", err)
			}
			if parsedID != tc.wantID {
				t.Fatalf("parsed tool id = %q, want %q", parsedID, tc.wantID)
			}
			if leaf != tc.wantName {
				t.Fatalf("parsed tool name = %q, want %q", leaf, tc.wantName)
			}

			resolved, kind, err := ResolveToolDeclByName([]ToolDecl{tc.tool}, projected, string(tc.wantKind))
			if err != nil {
				t.Fatalf("ResolveToolDeclByName returned error: %v", err)
			}
			if kind != string(tc.wantKind) {
				t.Fatalf("resolved kind = %q, want %q", kind, tc.wantKind)
			}
			if resolved.ToolID() != tc.wantID {
				t.Fatalf("resolved tool id = %q, want %q", resolved.ToolID(), tc.wantID)
			}

			resolvedByID, resolvedKind, err := ResolveToolDeclByID([]ToolDecl{tc.tool}, tc.wantID, string(tc.wantKind))
			if err != nil {
				t.Fatalf("ResolveToolDeclByID returned error: %v", err)
			}
			if resolvedByID.ToolID() != tc.wantID {
				t.Fatalf("resolved-by-id tool id = %q, want %q", resolvedByID.ToolID(), tc.wantID)
			}
			if resolvedKind != string(tc.wantKind) {
				t.Fatalf("resolved-by-id kind = %q, want %q", resolvedKind, tc.wantKind)
			}

			if _, _, err := ResolveToolDeclByName([]ToolDecl{tc.tool}, tc.wantName, string(tc.wantKind)); !isCanonicalBadRequest(err) {
				t.Fatalf("expected unprojected name to fail closed, got %v", err)
			}
		})
	}
}

func TestResolveToolDeclByName_RejectsMalformedProjectedNames(t *testing.T) {
	t.Parallel()

	tool := NewFunctionToolDecl(
		"codex/exec_command",
		"exec_command",
		"Run a shell command.",
		NewToolSchemaObject(`{"type":"object","properties":{"cmd":{"type":"string"}}}`),
	)
	if _, _, err := ResolveToolDeclByName([]ToolDecl{tool}, "exec_command__bogus", string(ToolKindFunction)); !isCanonicalBadRequest(err) {
		t.Fatalf("expected malformed projected name to fail closed, got %v", err)
	}
}

func TestParseToolPolicyMode_RejectsUnknownValues(t *testing.T) {
	t.Parallel()

	if _, ok := ParseToolPolicyMode("future_mode"); ok {
		t.Fatal("expected unknown mode to fail closed")
	}
}

func TestDecodeToolPolicyMetadata_RejectsUnknownMode(t *testing.T) {
	t.Parallel()

	if _, err := decodeToolPolicyMetadata(`{"mode":"future_mode"}`); !isCanonicalBadRequest(err) {
		t.Fatalf("expected BAD_REQUEST, got %v", err)
	}
}

func jsonObjectsEqual(gotRaw, wantRaw string) bool {
	var got any
	var want any
	if err := json.Unmarshal([]byte(gotRaw), &got); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(wantRaw), &want); err != nil {
		return false
	}
	return reflect.DeepEqual(got, want)
}

func isCanonicalBadRequest(err error) bool {
	var typed Error
	return errors.As(err, &typed) && typed.Code == ErrorCodeBadRequest
}
