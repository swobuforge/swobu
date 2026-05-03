package httpapi

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func TestDecodeResponsesToolMode_KnownValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want compatibility.ToolMode
	}{
		{name: "empty", raw: "", want: compatibility.ToolModeDefault},
		{name: "null", raw: "null", want: compatibility.ToolModeDefault},
		{name: "string none", raw: `"none"`, want: compatibility.ToolModeDefault},
		{name: "string auto", raw: `"auto"`, want: compatibility.ToolModeAuto},
		{name: "string required", raw: `"required"`, want: compatibility.ToolModeRequired},
		{name: "object auto", raw: `{"type":"auto"}`, want: compatibility.ToolModeAuto},
		{name: "object required", raw: `{"type":"required"}`, want: compatibility.ToolModeRequired},
		{name: "object function", raw: `{"type":"function","name":"grep"}`, want: compatibility.ToolModeRequired},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeResponsesToolMode(rawJSON(tc.raw))
			if err != nil {
				t.Fatalf("decodeResponsesToolMode returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("tool mode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecodeResponsesToolMode_UnknownValuesDowngradeToDefault(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`"future_mode"`,
		`{"type":"future_mode"}`,
		`{"type":"none"}`,
		`{"type":""}`,
		`{}`,
	} {
		got, err := decodeResponsesToolMode(rawJSON(raw))
		if err != nil {
			t.Fatalf("raw=%s decodeResponsesToolMode returned error: %v", raw, err)
		}
		if got != compatibility.ToolModeDefault {
			t.Fatalf("raw=%s tool mode = %q, want %q", raw, got, compatibility.ToolModeDefault)
		}
	}
}

func TestDecodeResponsesToolMode_InvalidShapesFailBadRequest(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`[]`,
		`42`,
		`true`,
		`{"type":{}}`,
		`{"type":[]}`,
		`{`,
	} {
		_, err := decodeResponsesToolMode(rawJSON(raw))
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
	var typed compatibility.Error
	return errors.As(err, &typed) && typed.Code == compatibility.ErrorCodeBadRequest
}
