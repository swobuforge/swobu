package httpapi

import (
	"encoding/json"
	"errors"
	"testing"

	responses "github.com/swobuforge/swobu/internal/adapters/wire/families/responses"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponsesToolPolicy_KnownValues(t *testing.T) {
	t.Parallel()

	functionTool := canonical.NewFunctionToolDecl("tool_0", "grep", "search text", canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`))
	customTool := canonical.NewCustomToolDecl("apply_patch", "apply_patch", "edit files", canonical.NewToolFormatObject(`{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}`))
	wantFunctionSpecific := functionTool.ToolID().String()
	wantCustomSpecific := canonical.NewSemanticToolIDFor(canonical.ToolOriginRequest, canonical.ToolKindCustom, "apply_patch").String()
	tests := []struct {
		name         string
		raw          string
		tools        []canonical.ToolDecl
		wantMode     canonical.ToolPolicyMode
		wantSpecific string
		wantType     string
	}{
		{name: "empty", raw: "", wantMode: canonical.ToolPolicyNone},
		{name: "null", raw: "null", wantMode: canonical.ToolPolicyNone},
		{
			name:     "empty with tools defaults to auto",
			raw:      "",
			tools:    []canonical.ToolDecl{functionTool},
			wantMode: canonical.ToolPolicyAuto,
		},
		{
			name:     "null with tools defaults to auto",
			raw:      "null",
			tools:    []canonical.ToolDecl{functionTool},
			wantMode: canonical.ToolPolicyAuto,
		},
		{name: "string none", raw: `"none"`, wantMode: canonical.ToolPolicyNone},
		{name: "string auto", raw: `"auto"`, wantMode: canonical.ToolPolicyAuto},
		{name: "string required", raw: `"required"`, wantMode: canonical.ToolPolicyRequired},
		{name: "object auto", raw: `{"type":"auto"}`, wantMode: canonical.ToolPolicyAuto},
		{name: "object required", raw: `{"type":"required"}`, wantMode: canonical.ToolPolicyRequired},
		{
			name:         "object function",
			raw:          `{"type":"function","name":"grep"}`,
			tools:        []canonical.ToolDecl{functionTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: wantFunctionSpecific,
			wantType:     "function",
		},
		{
			name:         "object custom",
			raw:          `{"type":"custom","name":"apply_patch"}`,
			tools:        []canonical.ToolDecl{customTool},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: wantCustomSpecific,
			wantType:     "custom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := responses.DecodeResponsesToolPolicy(rawJSON(tc.raw), tc.tools, nil, "")
			if err != nil {
				t.Fatalf("decodeResponsesToolPolicy returned error: %v", err)
			}
			if got.Mode != tc.wantMode {
				t.Fatalf("tool policy mode = %q, want %q", got.Mode, tc.wantMode)
			}
			if tc.wantSpecific == "" {
				if specific, ok := got.SpecificID(); ok {
					t.Fatalf("tool policy specific = %q, want none", specific)
				}
				if specificType, ok := got.SpecificToolType(); ok {
					t.Fatalf("tool policy specific type = %q, want none", specificType)
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

func TestDecodeResponsesToolPolicy_UnknownValuesFailClosed(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`"future_mode"`,
		`{"type":"future_mode"}`,
		`{"type":"none"}`,
		`{"type":""}`,
		`{}`,
	} {
		got, err := responses.DecodeResponsesToolPolicy(rawJSON(raw), nil, nil, "")
		if !isBadRequestError(err) {
			t.Fatalf("raw=%s err=%v, want BAD_REQUEST", raw, err)
		}
		if got.Mode != "" {
			t.Fatalf("raw=%s tool policy mode = %q, want empty", raw, got.Mode)
		}
	}
}

func TestDecodeResponsesToolPolicy_InvalidShapesFailBadRequest(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`[]`,
		`42`,
		`true`,
		`{"type":{}}`,
		`{"type":[]}`,
		`{"type":"function"}`,
		`{"type":"future_mode","name":"grep"}`,
		`{`,
	} {
		_, err := responses.DecodeResponsesToolPolicy(rawJSON(raw), nil, nil, "")
		if !isBadRequestError(err) {
			t.Fatalf("raw=%s err=%v, want BAD_REQUEST", raw, err)
		}
	}
}

func rawJSON(raw string) json.RawMessage {
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
}

func isBadRequestError(err error) bool {
	var typed canonical.Error
	return errors.As(err, &typed) && typed.Code == canonical.ErrorCodeBadRequest
}
