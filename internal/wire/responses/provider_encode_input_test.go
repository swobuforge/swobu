package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestEncode_UsesNativeReplayWhenPresent(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Turn:  canonical.NewTurnRef("client_prev"),
	})

	// NativeReplay present: encoder uses NativeReplay.Value, ignores client TurnRef.
	input := wire.ProviderEncodeInput{
		Request:      req,
		NativeReplay: &replay.NativeRef{Value: "native_prev_99"},
	}
	result, err := ProviderRequestDocumentEncoder{}.EncodeProviderRequestDocument(input, delivery.BufferedDelivery(), "ex-1")
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(result.Value.RawBytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := body["previous_response_id"]; got != "native_prev_99" {
		t.Fatalf("previous_response_id=%q, want native_prev_99", got)
	}
}

func TestEncode_DoesNotUseRequestTurnAsPreviousResponseIDWhenNativeReplayNil(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Turn:  canonical.NewTurnRef("client_prev"),
	})

	// NativeReplay nil means the encoder must not synthesize previous_response_id
	// from TurnRef. Provider continuation comes only from NativeReplay.Value.
	input := wire.ProviderEncodeInput{Request: req, NativeReplay: nil}

	result, err := ProviderRequestDocumentEncoder{}.EncodeProviderRequestDocument(input, delivery.BufferedDelivery(), "ex-2")
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(result.Value.RawBytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["previous_response_id"]; ok {
		t.Fatalf("expected NO previous_response_id when NativeReplay is nil; full body: %s", string(result.Value.RawBytes()))
	}
}

func TestEncode_EncodesCanonicalRequestAlways(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "test-model",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hello"),
			canonical.NewTextItem(canonical.ItemAuthorAssistant, "world"),
		},
	})

	input := wire.ProviderEncodeInput{Request: req}
	result, err := ProviderRequestDocumentEncoder{}.EncodeProviderRequestDocument(input, delivery.BufferedDelivery(), "ex-3")
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(result.Value.RawBytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["model"] != "test-model" {
		t.Fatalf("model=%q, want test-model", body["model"])
	}
	if body["input"] == nil {
		t.Fatalf("input should be present for non-empty thread")
	}
}
