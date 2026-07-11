package messages

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
		Model: "claude",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Tools: []canonical.ToolDecl{
			canonical.NewFunctionToolDecl("tool_0", "Read", "read files", canonical.NewToolSchemaObject(`{"type":"object","properties":{"path":{"type":"string"}}}`)),
		},
		CacheIntent: canonical.NewCacheIntent(canonical.CacheIntentParams{Key: "repo", Retention: canonical.CacheRetention1H}),
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
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool declaration", body["tools"])
	}
}

func TestDecodeRequest_IgnoresPromptCacheFields(t *testing.T) {
	codec := ClientRequestDecoder{}
	req := []byte(`{"model":"claude","prompt_cache_key":"repo","messages":[{"role":"user","content":"hi"}]}`)
	got, _, err := codec.DecodeClientRequest(carrier.WireDocument{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if !got.CacheIntent().IsZero() {
		t.Fatalf("cache intent=%+v want zero", got.CacheIntent())
	}
}
