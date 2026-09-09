package canonical

import "testing"

func TestOutputFormatJSONObjectCarriesNoInventedSchema(t *testing.T) {
	format, err := NewOutputFormat(OutputFormatParams{Kind: OutputFormatJSONObject})
	if err != nil {
		t.Fatal(err)
	}
	if format.Kind != OutputFormatJSONObject || !format.Schema.IsEmpty() {
		t.Fatalf("json_object format = %#v", format)
	}
	if _, err := NewOutputFormat(OutputFormatParams{
		Kind: OutputFormatJSONObject, Schema: NewRawJSONObject(`{"type":"object"}`),
	}); err == nil {
		t.Fatal("json_object accepted an invented schema")
	}
}

func TestOutputFormat_JSONSchemaMetadataRoundTrips(t *testing.T) {
	format, err := NewOutputFormat(OutputFormatParams{
		Kind:           OutputFormatJSONSchema,
		Name:           "reply_shape",
		Description:    "structured reply",
		Schema:         NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`),
		SchemaContract: SchemaContract{Profile: SchemaProfileOpenAI, Conformance: SchemaConformanceEnforced},
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}

	clone := format.Clone()
	if clone.Kind != OutputFormatJSONSchema || clone.Name != "reply_shape" || clone.Description != "structured reply" || clone.Schema.RawObject() != `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}` || clone.Conformance() != SchemaConformanceEnforced {
		t.Fatalf("Clone() = %#v, want json schema output format", clone)
	}

}

func TestOutputFormat_PreservesArbitraryJSONObjectSchemas(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{name: "reference", schema: `{"$ref":"#/$defs/reply","$defs":{"reply":{"type":"object"}}}`},
		{name: "composition", schema: `{"oneOf":[{"type":"string"},{"type":"number"}]}`},
		{name: "nested definitions", schema: `{"$defs":{"reply":{"$defs":{"answer":{"type":"string"}},"$ref":"#/$defs/answer"}}}`},
		{name: "annotations", schema: `{"type":"string","title":"answer","examples":["yes"]}`},
		{name: "unknown extension", schema: `{"type":"object","x-provider-future":{"mode":"useful"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, err := NewOutputFormat(OutputFormatParams{
				Kind:           OutputFormatJSONSchema,
				Name:           "reply_shape",
				Schema:         NewRawJSONObject(tt.schema),
				SchemaContract: SchemaContract{Profile: SchemaProfileOpenAI},
			})
			if err != nil {
				t.Fatalf("NewOutputFormat returned error: %v", err)
			}
			if got := format.Schema.RawObject(); got != tt.schema {
				t.Fatalf("schema = %s, want exact preservation of %s", got, tt.schema)
			}
		})
	}
}

func TestOutputFormat_RejectsInvalidSchemaContainers(t *testing.T) {
	for _, schema := range []string{"", "null", "{", "[]", `"string"`, "17"} {
		t.Run(schema, func(t *testing.T) {
			_, err := NewOutputFormat(OutputFormatParams{
				Kind:           OutputFormatJSONSchema,
				Name:           "reply_shape",
				Schema:         NewRawJSONObject(schema),
				SchemaContract: SchemaContract{Profile: SchemaProfileOpenAI},
			})
			if err == nil {
				t.Fatalf("NewOutputFormat(%q) returned nil error, want invalid schema container failure", schema)
			}
		})
	}
}

func TestOutputFormat_TextRejectsStructuredFields(t *testing.T) {
	_, err := NewOutputFormat(OutputFormatParams{
		Kind:           OutputFormatText,
		Name:           "unexpected",
		Description:    "unexpected",
		Schema:         NewRawJSONObject(`{"type":"object"}`),
		SchemaContract: SchemaContract{Profile: SchemaProfileOpenAI, Conformance: SchemaConformanceEnforced},
	})
	if err == nil {
		t.Fatal("NewOutputFormat returned nil error, want text format validation failure")
	}
}

func TestOutputFormat_NonSchemaKindsRejectSchemaContract(t *testing.T) {
	for _, kind := range []OutputFormatKind{OutputFormatUnspecified, OutputFormatText, OutputFormatJSONObject} {
		_, err := NewOutputFormat(OutputFormatParams{
			Kind:           kind,
			SchemaContract: SchemaContract{Profile: SchemaProfileOpenAI, Conformance: SchemaConformanceEnforced},
		})
		if err == nil {
			t.Fatalf("kind %q accepted a schema contract without a schema", kind)
		}
	}
	if (OutputFormat{SchemaContract: SchemaContract{Profile: SchemaProfileOpenAI}}).IsZero() {
		t.Fatal("output format with a schema contract reported zero")
	}
}
