package openaifamily

import (
	"strings"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

// ProviderUsageDecoder decodes provider-specific usage semantics from a raw
// protocol usage envelope into canonical token usage.
type ProviderUsageDecoder interface {
	DecodeToCanonical(raw RawUsageEnvelope, current canonical.TokenUsage) (canonical.TokenUsage, []ports.DegradationWarning)
}

// ProviderRoutePolicy owns provider route semantics while openaifamily owns shared
// transport/protocol execution mechanics.
type ProviderRoutePolicy interface {
	ProviderID() providercatalog.ProviderID
	BuildEncodePatches(req canonical.CanonicalRequest) ([]core.WirePatch, []ports.DegradationWarning)
	UsageDecoder() ProviderUsageDecoder
	DecodePatches() []core.WirePatch
}

type openAIProviderRoutePolicy struct{}
type ollamaProviderRoutePolicy struct{}
type openAICompatibleProviderRoutePolicy struct{}
type openRouterProviderRoutePolicy struct{}
type passthroughProviderUsageDecoder struct{}

func (passthroughProviderUsageDecoder) DecodeToCanonical(_ RawUsageEnvelope, current canonical.TokenUsage) (canonical.TokenUsage, []ports.DegradationWarning) {
	return current, nil
}

func (openAIProviderRoutePolicy) ProviderID() providercatalog.ProviderID {
	return providercatalog.ProviderSpecOpenAI
}

func (openAIProviderRoutePolicy) BuildEncodePatches(req canonical.CanonicalRequest) ([]core.WirePatch, []ports.DegradationWarning) {
	return routeCacheAffinityPatches(req), nil
}
func (openAIProviderRoutePolicy) UsageDecoder() ProviderUsageDecoder {
	return passthroughProviderUsageDecoder{}
}
func (openAIProviderRoutePolicy) DecodePatches() []core.WirePatch {
	return nil
}

func (ollamaProviderRoutePolicy) ProviderID() providercatalog.ProviderID {
	return providercatalog.ProviderSpecOllama
}

func (ollamaProviderRoutePolicy) BuildEncodePatches(req canonical.CanonicalRequest) ([]core.WirePatch, []ports.DegradationWarning) {
	intent := req.CacheIntent()
	key := strings.TrimSpace(intent.Key()) // swobu:io-string source=boundary
	if intent.Retention() != canonical.CacheRetentionUnset {
		return []core.WirePatch{
				NewCacheAffinityWirePatch(key, ""),
			}, []ports.DegradationWarning{{
				Code:   "cache_retention_unsupported",
				Field:  "cache.retention",
				Reason: "ollama route does not expose prompt cache retention control",
			}}
	}
	return []core.WirePatch{
		NewCacheAffinityWirePatch(key, strings.TrimSpace(string(intent.Retention()))), // swobu:io-string source=boundary
	}, nil
}
func (ollamaProviderRoutePolicy) UsageDecoder() ProviderUsageDecoder {
	return passthroughProviderUsageDecoder{}
}
func (ollamaProviderRoutePolicy) DecodePatches() []core.WirePatch {
	return nil
}

func (openAICompatibleProviderRoutePolicy) ProviderID() providercatalog.ProviderID {
	return providercatalog.ProviderSpecOpenAICompatible
}

func (openAICompatibleProviderRoutePolicy) BuildEncodePatches(req canonical.CanonicalRequest) ([]core.WirePatch, []ports.DegradationWarning) {
	return routeCacheAffinityPatches(req), nil
}
func (openAICompatibleProviderRoutePolicy) UsageDecoder() ProviderUsageDecoder {
	return passthroughProviderUsageDecoder{}
}
func (openAICompatibleProviderRoutePolicy) DecodePatches() []core.WirePatch {
	return nil
}

func (openRouterProviderRoutePolicy) ProviderID() providercatalog.ProviderID {
	return providercatalog.ProviderSpecOpenRouter
}

func (openRouterProviderRoutePolicy) BuildEncodePatches(req canonical.CanonicalRequest) ([]core.WirePatch, []ports.DegradationWarning) {
	return routeCacheAffinityPatches(req), nil
}
func (openRouterProviderRoutePolicy) UsageDecoder() ProviderUsageDecoder {
	return passthroughProviderUsageDecoder{}
}
func (openRouterProviderRoutePolicy) DecodePatches() []core.WirePatch {
	return nil
}

func routeCacheAffinityPatches(req canonical.CanonicalRequest) []core.WirePatch {
	return []core.WirePatch{
		NewCacheAffinityWirePatch(
			strings.TrimSpace(req.CacheIntent().Key()),               // swobu:io-string source=boundary
			strings.TrimSpace(string(req.CacheIntent().Retention())), // swobu:io-string source=boundary
		),
	}
}

func NewOpenAIPolicy() ProviderRoutePolicy { return openAIProviderRoutePolicy{} }

func NewOllamaPolicy() ProviderRoutePolicy { return ollamaProviderRoutePolicy{} }

func NewOpenAICompatiblePolicy() ProviderRoutePolicy {
	return openAICompatibleProviderRoutePolicy{}
}

func NewOpenRouterPolicy() ProviderRoutePolicy { return openRouterProviderRoutePolicy{} }
