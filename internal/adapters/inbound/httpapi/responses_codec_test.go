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

	specificID := canonical.NewSemanticToolID("tool_0")
	tests := []struct {
		name         string
		raw          string
		tools        []canonical.ToolDecl
		wantMode     canonical.ToolPolicyMode
		wantSpecific string
	}{
		{name: "empty", raw: "", wantMode: canonical.ToolPolicyNone},
		{name: "null", raw: "null", wantMode: canonical.ToolPolicyNone},
		{name: "string none", raw: `"none"`, wantMode: canonical.ToolPolicyNone},
		{name: "string auto", raw: `"auto"`, wantMode: canonical.ToolPolicyAuto},
		{name: "string required", raw: `"required"`, wantMode: canonical.ToolPolicyRequired},
		{name: "object auto", raw: `{"type":"auto"}`, wantMode: canonical.ToolPolicyAuto},
		{name: "object required", raw: `{"type":"required"}`, wantMode: canonical.ToolPolicyRequired},
		{
			name:         "object function",
			raw:          `{"type":"function","name":"grep"}`,
			tools:        []canonical.ToolDecl{canonical.NewFunctionToolDecl(string(specificID), "grep", "search text", canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`))},
			wantMode:     canonical.ToolPolicySpecific,
			wantSpecific: specificID.String(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := responses.DecodeResponsesToolPolicy(rawJSON(tc.raw), tc.tools)
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

func TestDecodeResponsesToolPolicy_UnknownValuesFailClosed(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`"future_mode"`,
		`{"type":"future_mode"}`,
		`{"type":"none"}`,
		`{"type":""}`,
		`{}`,
	} {
		got, err := responses.DecodeResponsesToolPolicy(rawJSON(raw), nil)
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
		`{`,
	} {
		_, err := responses.DecodeResponsesToolPolicy(rawJSON(raw), nil)
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
