package canonical

import "testing"

func TestOutputFormat_JSONSchemaMetadataRoundTrips(t *testing.T) {
	format, err := NewOutputFormat(OutputFormatParams{
		Kind:        OutputFormatJSONSchema,
		Name:        "reply_shape",
		Description: "structured reply",
		Schema:      NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`),
		Strict:      true,
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}

	clone := format.Clone()
	if clone.Kind != OutputFormatJSONSchema || clone.Name != "reply_shape" || clone.Description != "structured reply" || clone.Schema.RawObject() != `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}` || !clone.Strict {
		t.Fatalf("Clone() = %#v, want json schema output format", clone)
	}

	raw, err := encodeOutputFormatMetadata(format)
	if err != nil {
		t.Fatalf("encodeOutputFormatMetadata returned error: %v", err)
	}
	got, err := decodeOutputFormatMetadata(raw)
	if err != nil {
		t.Fatalf("decodeOutputFormatMetadata returned error: %v", err)
	}
	if got.Kind != OutputFormatJSONSchema || got.Name != "reply_shape" || got.Description != "structured reply" || got.Schema.RawObject() != `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}` || !got.Strict {
		t.Fatalf("decoded output format = %#v, want json schema output format", got)
	}
}

func TestOutputFormat_RejectsUnsupportedSchemaKeywords(t *testing.T) {
	_, err := NewOutputFormat(OutputFormatParams{
		Kind:   OutputFormatJSONSchema,
		Name:   "bad_schema",
		Schema: NewRawJSONObject(`{"oneOf":[{"type":"string"},{"type":"number"}]}`),
	})
	if err == nil {
		t.Fatal("NewOutputFormat returned nil error, want unsupported keyword failure")
	}
}

func TestOutputFormat_TextRejectsStructuredFields(t *testing.T) {
	_, err := NewOutputFormat(OutputFormatParams{
		Kind:        OutputFormatText,
		Name:        "unexpected",
		Description: "unexpected",
		Schema:      NewRawJSONObject(`{"type":"object"}`),
		Strict:      true,
	})
	if err == nil {
		t.Fatal("NewOutputFormat returned nil error, want text format validation failure")
	}
}
