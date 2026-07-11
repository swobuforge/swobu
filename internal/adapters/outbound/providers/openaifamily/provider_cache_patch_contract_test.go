package openaifamily

import (
	"encoding/json"
	"testing"

	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/transform"
)

func TestProviderCacheTransform_ProtocolEncodeStaysNeutral(t *testing.T) {
	codecs := []protocolkind.ProtocolKind{protocolkind.Responses, protocolkind.ChatCompletions, protocolkind.Messages, protocolkind.Completions}
	for _, kind := range codecs {
		codec, err := protocolregistry.ForProviderRequestProtocolCarrier(kind)
		if err != nil {
			t.Fatalf("codec(%s): %v", kind, err)
		}
		req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: "m", Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")}, InputText: "hi"})
		wire, err := codec.EncodeProviderRequest(req, delivery.BufferedDelivery())
		if err != nil {
			t.Fatalf("EncodeProviderRequest(%s): %v", kind, err)
		}
		body := map[string]any{}
		if err := json.Unmarshal(wire.Raw, &body); err != nil {
			t.Fatalf("json.Unmarshal(%s): %v", kind, err)
		}
		if _, ok := body["prompt_cache_key"]; ok {
			t.Fatalf("prompt_cache_key must not be emitted by protocol encoder kind=%s", kind)
		}
	}
}

func TestProviderCacheTransform_ProfileFactRecordApplyProviderFields(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: "m", CacheIntent: canonical.NewCacheIntent(canonical.CacheIntentParams{Key: "repo-alpha", Retention: canonical.CacheRetention24H})})
	in := carrier.WireDocument{Raw: []byte(`{"model":"m"}`)}
	out, _, _, _, err := transform.ApplyProviderWireOutStage(in, newTransformRegistry(NewOpenAIPolicy().Facts(req)))
	if err != nil {
		t.Fatalf("ApplyDocumentStage: %v", err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(out.Raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, _ := payload["prompt_cache_key"].(string); got != "repo-alpha" {
		t.Fatalf("prompt_cache_key=%q", got)
	}
}
