package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestEncode_PreservesGenerationControls(t *testing.T) {
	maxTokens := 64
	temperature := 0.25
	topP := 0.9
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
		Temperature:     &temperature,
		TopP:            &topP,
		StopSequences:   []string{"END", "DONE"},
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    canonical.Specify("claude-3-5"),
		Items:    []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
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
	if got, ok := body["max_completion_tokens"].(float64); !ok || got != 64 {
		t.Fatalf("max_completion_tokens = %#v, want 64", body["max_completion_tokens"])
	}
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("max_tokens = %#v, want absent", body["max_tokens"])
	}
	if got, ok := body["temperature"].(float64); !ok || got != 0.25 {
		t.Fatalf("temperature = %#v, want 0.25", body["temperature"])
	}
	if got, ok := body["top_p"].(float64); !ok || got != 0.9 {
		t.Fatalf("top_p = %#v, want 0.9", body["top_p"])
	}
	stop, ok := body["stop"].([]any)
	if !ok || len(stop) != 2 {
		t.Fatalf("stop = %#v, want two stop sequences", body["stop"])
	}
}

func TestEncode_OmitsMaxCompletionTokensWhenMaxOutputTokensUnset(t *testing.T) {
	t.Parallel()

	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-5.4"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier returned error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if _, ok := body["max_completion_tokens"]; ok {
		t.Fatalf("max_completion_tokens = %#v, want absent when max output tokens unset", body["max_completion_tokens"])
	}
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("max_tokens = %#v, want absent when max output tokens unset", body["max_tokens"])
	}
}

func TestDecodeRequest_DecodesGenerationControls(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"claude-3-5","messages":[{"role":"user","content":"hi"}],"max_tokens":64,"temperature":0.25,"top_p":0.9,"stop":["END","DONE"]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	if max, ok := got.Controls().Limits.MaxOutputTokens.Value(); !ok || max != 64 {
		t.Fatalf("max_tokens = (%d, %v), want (64, true)", max, ok)
	}
	if temp, ok := got.Controls().Sampling.Temperature.Value(); !ok || temp != 0.25 {
		t.Fatalf("temperature = (%v, %v), want (0.25, true)", temp, ok)
	}
	if topP, ok := got.Controls().Sampling.TopP.Value(); !ok || topP != 0.9 {
		t.Fatalf("top_p = (%v, %v), want (0.9, true)", topP, ok)
	}
	if gotStop := got.Controls().Limits.StopSequences; len(gotStop) != 2 || gotStop[0] != "END" || gotStop[1] != "DONE" {
		t.Fatalf("stop sequences = %#v, want [END DONE]", gotStop)
	}
}

func TestDecodeRequest_DecodesGPT5GenerationControls(t *testing.T) {
	t.Parallel()

	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}],"max_completion_tokens":64,"temperature":0.25,"top_p":0.9}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	if max, ok := got.Controls().Limits.MaxOutputTokens.Value(); !ok || max != 64 {
		t.Fatalf("max_output_tokens = (%d, %v), want (64, true)", max, ok)
	}
	if temp, ok := got.Controls().Sampling.Temperature.Value(); !ok || temp != 0.25 {
		t.Fatalf("temperature = (%v, %v), want (0.25, true)", temp, ok)
	}
	if topP, ok := got.Controls().Sampling.TopP.Value(); !ok || topP != 0.9 {
		t.Fatalf("top_p = (%v, %v), want (0.9, true)", topP, ok)
	}
}

func TestDecodeRequest_MaxCompletionTokensExplicitlyPrecedesLegacyMaxTokens(t *testing.T) {
	t.Parallel()

	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"hi"}],"max_tokens":32,"max_completion_tokens":64}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	if max, ok := got.Controls().Limits.MaxOutputTokens.Value(); !ok || max != 64 {
		t.Fatalf("max output tokens = (%d, %v), want explicit max_completion_tokens value 64", max, ok)
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
		Model:        canonical.Specify("claude-3-5"),
		Items:        []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		OutputFormat: canonical.Specify(format),
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier returned error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	responseFormat, ok := body["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format = %#v, want object", body["response_format"])
	}
	if got := responseFormat["type"]; got != "json_schema" {
		t.Fatalf("response_format.type = %#v, want json_schema", got)
	}
	jsonSchema, ok := responseFormat["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("response_format.json_schema = %#v, want object", responseFormat["json_schema"])
	}
	if got := jsonSchema["name"]; got != "reply_shape" {
		t.Fatalf("json_schema.name = %#v, want reply_shape", got)
	}
	if got := jsonSchema["description"]; got != "structured reply" {
		t.Fatalf("json_schema.description = %#v, want structured reply", got)
	}
	if got, ok := jsonSchema["strict"].(bool); !ok || !got {
		t.Fatalf("json_schema.strict = %#v, want true", jsonSchema["strict"])
	}
	schema, ok := jsonSchema["schema"].(map[string]any)
	if !ok {
		t.Fatalf("json_schema.schema = %#v, want object", jsonSchema["schema"])
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
	req := []byte(`{"model":"claude-3-5","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"reply_shape","description":"structured reply","schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false},"strict":true}}}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	format := got.OutputFormat()
	if format.Kind != canonical.OutputFormatJSONSchema || format.Name != "reply_shape" || format.Description != "structured reply" || format.Schema.RawObject() != `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}` || !format.Strict {
		t.Fatalf("output format = %#v, want json schema", format)
	}
}
