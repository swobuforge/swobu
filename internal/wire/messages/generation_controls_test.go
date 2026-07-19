package messages

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
	maxTokens := 88
	temperature := 0.3
	topP := 0.75
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
		Temperature:     &temperature,
		TopP:            &topP,
		StopSequences:   []string{"END"},
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    "claude-3-5",
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
	if got, ok := body["max_tokens"].(float64); !ok || got != 88 {
		t.Fatalf("max_tokens = %#v, want 88", body["max_tokens"])
	}
	if got, ok := body["temperature"].(float64); !ok || got != 0.3 {
		t.Fatalf("temperature = %#v, want 0.3", body["temperature"])
	}
	if got, ok := body["top_p"].(float64); !ok || got != 0.75 {
		t.Fatalf("top_p = %#v, want 0.75", body["top_p"])
	}
	stop, ok := body["stop_sequences"].([]any)
	if !ok || len(stop) != 1 {
		t.Fatalf("stop_sequences = %#v, want one sequence", body["stop_sequences"])
	}
}

func TestEncode_PreservesInstructionsAsTopLevelSystem(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "claude-3-5",
		Instructions: "Use native tools for filesystem work.",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "inspect files")},
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier returned error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if got := body["system"]; got != "Use native tools for filesystem work." {
		t.Fatalf("system = %#v, want canonical instructions", got)
	}
}

func TestDecodeRequest_PreservesTopLevelSystemAsInstructions(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"claude-3-5","system":"Use native tools for filesystem work.","messages":[{"role":"user","content":"inspect files"}]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	if got.Instructions() != "Use native tools for filesystem work." {
		t.Fatalf("instructions = %q, want top-level system", got.Instructions())
	}
	items := got.Items()
	if len(items) != 1 || items[0].Text != "inspect files" {
		t.Fatalf("items = %#v, want user request only", items)
	}
}

func TestDecodeRequest_DecodesGenerationControls(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"claude-3-5","messages":[{"role":"user","content":"hi"}],"max_tokens":88,"temperature":0.3,"top_p":0.75,"stop_sequences":["END"]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	if max, ok := got.Controls().Limits.MaxOutputTokens.Value(); !ok || max != 88 {
		t.Fatalf("max_tokens = (%d, %v), want (88, true)", max, ok)
	}
	if temp, ok := got.Controls().Sampling.Temperature.Value(); !ok || temp != 0.3 {
		t.Fatalf("temperature = (%v, %v), want (0.3, true)", temp, ok)
	}
	if topP, ok := got.Controls().Sampling.TopP.Value(); !ok || topP != 0.75 {
		t.Fatalf("top_p = (%v, %v), want (0.75, true)", topP, ok)
	}
	if gotStop := got.Controls().Limits.StopSequences; len(gotStop) != 1 || gotStop[0] != "END" {
		t.Fatalf("stop_sequences = %#v, want [END]", gotStop)
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
		Model:        "claude-3-5",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		OutputFormat: format,
	}), delivery.BufferedDelivery())
	if err == nil || !strings.Contains(err.Error(), "structured output") {
		t.Fatalf("EncodeCarrier err=%v, want structured-output rejection", err)
	}
}

func TestDecodeRequest_RejectsStructuredOutputFormat(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"claude-3-5","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"reply_shape","schema":{"type":"object","properties":{"answer":{"type":"string"}}}}}}`)
	_, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err == nil || !strings.Contains(err.Error(), "structured output") {
		t.Fatalf("DecodeClientRequest err=%v, want structured-output rejection", err)
	}
}
