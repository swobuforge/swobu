package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestOutputFormatNativeStrictPresence(t *testing.T) {
	for _, field := range []string{"", `,"strict":false`, `,"strict":true`} {
		t.Run(field, func(t *testing.T) {
			raw := `{"format":{"type":"json_schema","name":"answer","schema":{"properties":{"middle_name":{"type":"string"}}}` + field + `}}`
			var source responsesTextDTO
			if err := json.Unmarshal([]byte(raw), &source); err != nil {
				t.Fatal(err)
			}
			format, err := decodeResponsesOutputFormat(&source, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			out, err := DefaultOutputFormatLowering(format, nil)
			if err != nil {
				t.Fatal(err)
			}
			if (source.Format.Strict == nil) != (out.Format.Strict == nil) {
				t.Fatal("strict presence changed")
			}
			if source.Format.Strict != nil && *source.Format.Strict != *out.Format.Strict {
				t.Fatal("strict value changed")
			}
			if string(source.Format.Schema) != string(out.Format.Schema) {
				t.Fatal("schema changed")
			}
		})
	}
}

func TestOutputFormatForeignContractExplicitlyRelaxes(t *testing.T) {
	schema := canonical.NewRawJSONObject(`{"properties":{"middle_name":{"type":"string"}}}`)
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind: canonical.OutputFormatJSONSchema, Schema: schema,
		SchemaContract: canonical.SchemaContract{Profile: canonical.SchemaProfileAnthropic, Conformance: canonical.SchemaConformanceEnforced},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := DefaultOutputFormatLowering(format, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Format.Strict == nil || *out.Format.Strict {
		t.Fatal("foreign contract must emit explicit strict=false")
	}
	if string(out.Format.Schema) != schema.RawObject() {
		t.Fatal("optional property schema changed")
	}
	if out.Format.Name != "swobu_output" {
		t.Fatalf("target-local name = %q", out.Format.Name)
	}
}

func TestOutputFormatSerializerDoesNotSynthesizeSchemaName(t *testing.T) {
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:           canonical.OutputFormatJSONSchema,
		Schema:         canonical.NewRawJSONObject(`{"type":"object"}`),
		SchemaContract: canonical.SchemaContract{Profile: canonical.SchemaProfileOpenAI},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := encodeResponsesOutputFormat(format, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Format.Name != "" {
		t.Fatalf("leaf serializer synthesized schema name %q", out.Format.Name)
	}
}
