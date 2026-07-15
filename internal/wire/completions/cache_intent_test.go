package completions

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestEncode_DoesNotEmbedProviderCacheFields(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-4o-mini",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	raw := wire.Raw
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key must be provider adapter concern")
	}
}

func TestDecodeRequest_IgnoresPromptCacheFields(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","prompt":"hi","prompt_cache_key":"repo"}`)
	got, _, err := codec.DecodeClientRequest(carrier.CarrierDocument{Family: protocolkind.Completions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if got.Model() != "gpt-4o-mini" {
		t.Fatalf("model = %q, want %q", got.Model(), "gpt-4o-mini")
	}
}

func TestDecodeRequest_IgnoresUnknownField(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","prompt":"hi","unexpected":true}`)
	got, _, err := codec.DecodeClientRequest(carrier.CarrierDocument{Family: protocolkind.Completions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest() error = %v", err)
	}
	if got.Model() != "gpt-4o-mini" {
		t.Fatalf("model = %q, want %q", got.Model(), "gpt-4o-mini")
	}
}
