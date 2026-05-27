package openaifamily

import (
	"encoding/json"
	"io"
	"testing"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func TestProviderCachePatch_ProtocolEncodeStaysNeutral(t *testing.T) {
	tests := []struct {
		name       string
		kind       protocolkind.ProtocolKind
		request    canonical.CanonicalRequest
		payloadKey string
	}{
		{
			name: "responses",
			kind: protocolkind.Responses,
			request: canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: "m",
				Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
			}),
		},
		{
			name: "chat_completions",
			kind: protocolkind.ChatCompletions,
			request: canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: "m",
				Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
			}),
		},
		{
			name: "messages",
			kind: protocolkind.Messages,
			request: canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: "m",
				Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
			}),
		},
		{
			name: "completions",
			kind: protocolkind.Completions,
			request: canonical.NewCanonicalRequest(canonical.RequestParams{
				Model:     "m",
				InputText: "hi",
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec, err := protocolregistry.ForProtocolKind(tc.kind)
			if err != nil {
				t.Fatalf("ForProtocolKind: %v", err)
			}
			wire, err := codec.EncodeRequest(tc.request, false)
			if err != nil {
				t.Fatalf("EncodeRequest: %v", err)
			}
			raw, err := io.ReadAll(wire.Body)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			body := map[string]any{}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("json.Unmarshal: %v raw=%s", err, string(raw))
			}
			if _, ok := body["prompt_cache_key"]; ok {
				t.Fatalf("prompt_cache_key must be provider patch concern raw=%s", string(raw))
			}
			if _, ok := body["prompt_cache_retention"]; ok {
				t.Fatalf("prompt_cache_retention must be provider patch concern raw=%s", string(raw))
			}
		})
	}
}

func TestProviderCachePatch_ProfileAppliesProviderFields(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		CacheIntent: canonical.NewCacheIntent(canonical.CacheIntentParams{
			Key:       "repo-alpha",
			Retention: canonical.CacheRetention24H,
		}),
	})

	tests := []struct {
		name          string
		profile       ProviderRoutePolicy
		wantRetention string
	}{
		{name: "openai", profile: NewOpenAIPolicy(), wantRetention: "24h"},
		{name: "ollama", profile: NewOllamaPolicy(), wantRetention: ""},
		{name: "openaicompat", profile: NewOpenAICompatiblePolicy(), wantRetention: "24h"},
		{name: "openrouter", profile: NewOpenRouterPolicy(), wantRetention: "24h"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			patches, warnings := tc.profile.BuildEncodePatches(req)
			packet := map[string]any{}
			wire := core.WirePacket{Payload: packet}
			for _, patch := range patches {
				if patch == nil {
					continue
				}
				if err := patch.ApplyEncode(&wire); err != nil {
					t.Fatalf("ApplyEncode: %v", err)
				}
			}
			if got, _ := packet["prompt_cache_key"].(string); got != "repo-alpha" {
				t.Fatalf("prompt_cache_key=%q want repo-alpha", got)
			}
			if got, _ := packet["prompt_cache_retention"].(string); got != tc.wantRetention {
				t.Fatalf("prompt_cache_retention=%q want %q", got, tc.wantRetention)
			}
			if len(warnings) > 0 && tc.profile.ProviderID() != providercatalog.ProviderSpecOllama {
				t.Fatalf("unexpected warnings: %#v", warnings)
			}
		})
	}
}
