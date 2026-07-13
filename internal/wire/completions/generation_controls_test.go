package completions

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
	maxTokens := 99
	temperature := 0.15
	topP := 0.7
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
		Model:    "gpt-3.5",
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
	if got, ok := body["max_tokens"].(float64); !ok || got != 99 {
		t.Fatalf("max_tokens = %#v, want 99", body["max_tokens"])
	}
	if got, ok := body["temperature"].(float64); !ok || got != 0.15 {
		t.Fatalf("temperature = %#v, want 0.15", body["temperature"])
	}
	if got, ok := body["top_p"].(float64); !ok || got != 0.7 {
		t.Fatalf("top_p = %#v, want 0.7", body["top_p"])
	}
	stop, ok := body["stop"].([]any)
	if !ok || len(stop) != 2 {
		t.Fatalf("stop = %#v, want two stop sequences", body["stop"])
	}
}

func TestDecodeRequest_DecodesGenerationControls(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-3.5","prompt":"hi","max_tokens":99,"temperature":0.15,"top_p":0.7,"stop":["END","DONE"]}`)
	got, _, err := codec.DecodeClientRequest(carrier.WireDocument{Family: protocolkind.Completions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	if max, ok := got.Controls().Limits.MaxOutputTokens.Value(); !ok || max != 99 {
		t.Fatalf("max_tokens = (%d, %v), want (99, true)", max, ok)
	}
	if temp, ok := got.Controls().Sampling.Temperature.Value(); !ok || temp != 0.15 {
		t.Fatalf("temperature = (%v, %v), want (0.15, true)", temp, ok)
	}
	if topP, ok := got.Controls().Sampling.TopP.Value(); !ok || topP != 0.7 {
		t.Fatalf("top_p = (%v, %v), want (0.7, true)", topP, ok)
	}
	if gotStop := got.Controls().Limits.StopSequences; len(gotStop) != 2 || gotStop[0] != "END" || gotStop[1] != "DONE" {
		t.Fatalf("stop sequences = %#v, want [END DONE]", gotStop)
	}
}

func TestEncode_RejectsStructuredOutputFormat(t *testing.T) {
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:   canonical.OutputFormatJSONSchema,
		Name:   "reply_shape",
		Schema: canonical.NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}
	_, err = EncodeCarrier(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "gpt-3.5",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		OutputFormat: format,
	}), delivery.BufferedDelivery())
	if err == nil || !strings.Contains(err.Error(), "structured output") {
		t.Fatalf("EncodeCarrier err=%v, want structured-output rejection", err)
	}
}

func TestDecodeRequest_RejectsStructuredOutputFormat(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-3.5","prompt":"hi","response_format":{"type":"json_schema","json_schema":{"name":"reply_shape","schema":{"type":"object","properties":{"answer":{"type":"string"}}}}}}`)
	_, _, err := codec.DecodeClientRequest(carrier.WireDocument{Family: protocolkind.Completions, Raw: req})
	if err == nil || !strings.Contains(err.Error(), "structured output") {
		t.Fatalf("DecodeClientRequest err=%v, want structured-output rejection", err)
	}
}
