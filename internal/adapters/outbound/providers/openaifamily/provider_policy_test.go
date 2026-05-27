package openaifamily

import (
	"net/http"
	"testing"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestProviderConstructors_ExposeExplicitProviderModules(t *testing.T) {
	if got := NewOpenAIPolicy().ProviderID(); got != providercatalog.ProviderSpecOpenAI {
		t.Fatalf("openai policy provider=%s", got)
	}
	if got := NewOllamaPolicy().ProviderID(); got != providercatalog.ProviderSpecOllama {
		t.Fatalf("ollama policy provider=%s", got)
	}
	if got := NewOpenAICompatiblePolicy().ProviderID(); got != providercatalog.ProviderSpecOpenAICompatible {
		t.Fatalf("openaicompat policy provider=%s", got)
	}
	if got := NewOpenRouterPolicy().ProviderID(); got != providercatalog.ProviderSpecOpenRouter {
		t.Fatalf("openrouter policy provider=%s", got)
	}
}

func TestProviderPolicy_BuildEncodePatches_UsesCanonicalCacheIntent(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		CacheIntent: canonical.NewCacheIntent(canonical.CacheIntentParams{
			Key:       "repo-alpha",
			Retention: canonical.CacheRetention24H,
		}),
	})
	profiles := []ProviderRoutePolicy{
		NewOpenAIPolicy(),
		NewOllamaPolicy(),
		NewOpenAICompatiblePolicy(),
		NewOpenRouterPolicy(),
	}
	for _, profile := range profiles {
		providerID := profile.ProviderID()
		patches, warnings := profile.BuildEncodePatches(req)
		packet := core.WirePacket{Payload: map[string]any{}}
		for _, patch := range patches {
			if patch == nil {
				continue
			}
			if err := patch.ApplyEncode(&packet); err != nil {
				t.Fatalf("provider=%s patch apply: %v", providerID, err)
			}
		}
		if got, _ := packet.Payload["prompt_cache_key"].(string); got != "repo-alpha" {
			t.Fatalf("provider=%s key=%q want repo-alpha", providerID, got)
		}
		wantRetention := "24h"
		if providerID == providercatalog.ProviderSpecOllama {
			wantRetention = ""
		}
		if got, _ := packet.Payload["prompt_cache_retention"].(string); got != wantRetention {
			t.Fatalf("provider=%s retention=%q want %q", providerID, got, wantRetention)
		}
		if len(warnings) > 0 && providerID != providercatalog.ProviderSpecOllama {
			t.Fatalf("provider=%s unexpected lowering warnings: %#v", providerID, warnings)
		}
	}
}

func TestProviderRoutePolicy_DecodeBuffered_UsesMandatoryProfileContract(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	for _, profile := range []ProviderRoutePolicy{
		NewOpenAIPolicy(),
		NewOllamaPolicy(),
		NewOpenAICompatiblePolicy(),
		NewOpenRouterPolicy(),
	} {
		if profile.UsageDecoder() == nil {
			t.Fatalf("provider=%s usage adapter is nil", profile.ProviderID())
		}
		out, warnings, err := decodeBufferedByKind(profile, protocolkind.ChatCompletions, raw, http.Header{})
		if err != nil {
			t.Fatalf("provider=%s decode: %v", profile.ProviderID(), err)
		}
		if out.Model() != "m" {
			t.Fatalf("provider=%s model=%q", profile.ProviderID(), out.Model())
		}
		if len(warnings) != 0 {
			t.Fatalf("provider=%s decode warnings=%#v", profile.ProviderID(), warnings)
		}
	}
}

type testDecodePatch struct {
	raw []byte
}

func (p testDecodePatch) ApplyEncode(*core.WirePacket) error { return nil }
func (p testDecodePatch) ApplyDecode(packet *core.WirePacket) error {
	packet.RawBody = p.raw
	return nil
}

type testProviderRoutePolicyWithPatch struct {
	providerID providercatalog.ProviderID
	patches    []core.WirePatch
}

func (p testProviderRoutePolicyWithPatch) ProviderID() providercatalog.ProviderID {
	return p.providerID
}
func (p testProviderRoutePolicyWithPatch) BuildEncodePatches(canonical.CanonicalRequest) ([]core.WirePatch, []ports.DegradationWarning) {
	return nil, nil
}
func (p testProviderRoutePolicyWithPatch) UsageDecoder() ProviderUsageDecoder {
	return passthroughProviderUsageDecoder{}
}
func (p testProviderRoutePolicyWithPatch) DecodePatches() []core.WirePatch {
	return p.patches
}

func TestProviderRoutePolicy_DecodeBuffered_AppliesWireDecodePatchesBeforeProtocolParse(t *testing.T) {
	profile := testProviderRoutePolicyWithPatch{
		providerID: providercatalog.ProviderSpecOpenAI,
		patches: []core.WirePatch{
			testDecodePatch{raw: []byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"patched"},"finish_reason":"stop"}]}`)},
		},
	}
	_, _, err := decodeBufferedByKind(profile, protocolkind.ChatCompletions, []byte(`{"broken":`), http.Header{})
	if err != nil {
		t.Fatalf("decode with patch should succeed: %v", err)
	}
}
