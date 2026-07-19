package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestEncode_PreservesGenerationControls(t *testing.T) {
	maxTokens := 128
	temperature := 0.4
	topP := 0.85
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
		Temperature:     &temperature,
		TopP:            &topP,
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    "gpt-4o-mini",
		Items:    []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Controls: controls,
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier returned error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if got, ok := body["max_output_tokens"].(float64); !ok || got != 128 {
		t.Fatalf("max_output_tokens = %#v, want 128", body["max_output_tokens"])
	}
	if got, ok := body["temperature"].(float64); !ok || got != 0.4 {
		t.Fatalf("temperature = %#v, want 0.4", body["temperature"])
	}
	if got, ok := body["top_p"].(float64); !ok || got != 0.85 {
		t.Fatalf("top_p = %#v, want 0.85", body["top_p"])
	}
}

func TestEncode_RejectsStopSequences(t *testing.T) {
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		StopSequences: []string{"END"},
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	_, err = EncodeCarrier(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    "gpt-4o-mini",
		Items:    []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Controls: controls,
	}), delivery.BufferedDelivery())
	if err == nil || !strings.Contains(err.Error(), "stop sequences") {
		t.Fatalf("EncodeCarrier err=%v, want explicit stop-sequence rejection", err)
	}
}

func TestDecodeRequest_DecodesGenerationControls(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","input":"hi","max_output_tokens":64,"temperature":0.2,"top_p":0.8}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	if max, ok := got.Controls().Limits.MaxOutputTokens.Value(); !ok || max != 64 {
		t.Fatalf("max_output_tokens = (%d, %v), want (64, true)", max, ok)
	}
	if temp, ok := got.Controls().Sampling.Temperature.Value(); !ok || temp != 0.2 {
		t.Fatalf("temperature = (%v, %v), want (0.2, true)", temp, ok)
	}
	if topP, ok := got.Controls().Sampling.TopP.Value(); !ok || topP != 0.8 {
		t.Fatalf("top_p = (%v, %v), want (0.8, true)", topP, ok)
	}
}

func TestDecodeRequest_RejectsStopSequences(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","input":"hi","stop":["END"]}`)
	_, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req})
	if err == nil || !strings.Contains(err.Error(), "stop sequences") {
		t.Fatalf("DecodeClientRequest err=%v, want explicit stop-sequence rejection", err)
	}
}

func TestEncode_PreservesStructuredOutputFormat(t *testing.T) {
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:        canonical.OutputFormatJSONSchema,
		Name:        "reply_shape",
		Description: "structured reply",
		Schema:      canonical.NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`),
		Strict:      true,
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "gpt-4o-mini",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		OutputFormat: format,
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier returned error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	text, ok := body["text"].(map[string]any)
	if !ok {
		t.Fatalf("text = %#v, want object", body["text"])
	}
	formatBody, ok := text["format"].(map[string]any)
	if !ok {
		t.Fatalf("text.format = %#v, want object", text["format"])
	}
	if got := formatBody["type"]; got != "json_schema" {
		t.Fatalf("format.type = %#v, want json_schema", got)
	}
	if got := formatBody["name"]; got != "reply_shape" {
		t.Fatalf("format.name = %#v, want reply_shape", got)
	}
	if got := formatBody["description"]; got != "structured reply" {
		t.Fatalf("format.description = %#v, want structured reply", got)
	}
	if got, ok := formatBody["strict"].(bool); !ok || !got {
		t.Fatalf("format.strict = %#v, want true", formatBody["strict"])
	}
	schema, ok := formatBody["schema"].(map[string]any)
	if !ok {
		t.Fatalf("format.schema = %#v, want object", formatBody["schema"])
	}
	if got := schema["type"]; got != "object" {
		t.Fatalf("schema.type = %#v, want object", got)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties = %#v, want object", schema["properties"])
	}
	answer, ok := properties["answer"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties.answer = %#v, want object", properties["answer"])
	}
	if got := answer["type"]; got != "string" {
		t.Fatalf("schema.properties.answer.type = %#v, want string", got)
	}
}

func TestDecodeRequest_DecodesStructuredOutputFormat(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","input":"hi","text":{"format":{"type":"json_schema","name":"reply_shape","description":"structured reply","schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false},"strict":true}}}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	format := got.OutputFormat()
	if format.Kind != canonical.OutputFormatJSONSchema || format.Name != "reply_shape" || format.Description != "structured reply" || format.Schema.RawObject() != `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}` || !format.Strict {
		t.Fatalf("output format = %#v, want json schema", format)
	}
}
