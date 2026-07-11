package completions

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestEncode_DoesNotEmbedProviderCacheFields(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:       "gpt-4o-mini",
		Items:       []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		CacheIntent: canonical.NewCacheIntent(canonical.CacheIntentParams{Key: "repo", Retention: canonical.CacheRetention24H}),
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
		t.Fatalf("prompt_cache_key must be provider transform concern")
	}
}

func TestDecodeRequest_IgnoresPromptCacheFields(t *testing.T) {
	codec := ClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","prompt":"hi","prompt_cache_key":"repo"}`)
	got, _, err := codec.DecodeClientRequest(carrier.WireDocument{Family: protocolkind.Completions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if !got.CacheIntent().IsZero() {
		t.Fatalf("cache intent=%+v want zero", got.CacheIntent())
	}
}

func TestDecodeRequest_RejectsUnknownField(t *testing.T) {
	codec := ClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","prompt":"hi","unexpected":true}`)
	_, _, err := codec.DecodeClientRequest(carrier.WireDocument{Family: protocolkind.Completions, Raw: req})
	if err == nil {
		t.Fatal("expected error")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
	if got := compatErr.Details["json_pointer"]; got != "/unexpected" {
		t.Fatalf("json_pointer = %q, want %q", got, "/unexpected")
	}
}
