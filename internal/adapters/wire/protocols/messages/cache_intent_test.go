package messages

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEncode_DoesNotEmbedProviderCacheFields(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:       "claude",
		Items:       []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		CacheIntent: canonical.NewCacheIntent(canonical.CacheIntentParams{Key: "repo", Retention: canonical.CacheRetention1H}),
	})
	wire, err := encodeRequest(req, false)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	raw, _ := io.ReadAll(wire.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key must be provider patch concern")
	}
}

func TestDecodeRequest_IgnoresPromptCacheFields(t *testing.T) {
	codec := MessagesFamilyCodec{}
	req := []byte(`{"model":"claude","prompt_cache_key":"repo","messages":[{"role":"user","content":"hi"}]}`)
	got, _, err := codec.DecodeRequest(req)
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if !got.CacheIntent().IsZero() {
		t.Fatalf("cache intent=%+v want zero", got.CacheIntent())
	}
}
