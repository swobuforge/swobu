package providercatalog

import "github.com/swobuforge/swobu/internal/domain/protocolkind"

// CachePrimitive is one request-side cache control primitive recognized by Swobu.
type CachePrimitive string

const (
	CachePrimitiveAutoPrefix    CachePrimitive = "auto_prefix"
	CachePrimitiveAffinityKey   CachePrimitive = "affinity_key"
	CachePrimitiveBreakpoint    CachePrimitive = "breakpoint"
	CachePrimitiveCacheRef      CachePrimitive = "cache_ref"
	CachePrimitiveRetentionHint CachePrimitive = "retention_hint"
)

// CacheCapabilityFact declares cache control primitives for one provider route.
// Facts are intentionally conservative and route-scoped.
type CacheCapabilityFact struct {
	ProviderID   ProviderID
	ProtocolKind protocolkind.ProtocolKind
	Primitives   []CachePrimitive
	Notes        string
}

// CacheCapabilityFacts returns route-scoped cache capability facts for
// providers currently declared in Swobu's runtime catalog.
func CacheCapabilityFacts() []CacheCapabilityFact {
	return []CacheCapabilityFact{
		{
			ProviderID:   ProviderSpecOpenAI,
			ProtocolKind: protocolkind.Responses,
			Primitives:   []CachePrimitive{CachePrimitiveAutoPrefix, CachePrimitiveAffinityKey, CachePrimitiveRetentionHint},
			Notes:        "OpenAI responses supports automatic prefix cache plus prompt_cache_key/retention where model supports it",
		},
		{
			ProviderID:   ProviderSpecOpenAICompatible,
			ProtocolKind: protocolkind.Responses,
			Primitives:   []CachePrimitive{CachePrimitiveAutoPrefix, CachePrimitiveAffinityKey, CachePrimitiveRetentionHint},
			Notes:        "OpenAI-compatible routes are treated as OpenAI-family cache controls",
		},
		{
			ProviderID:   ProviderSpecOllama,
			ProtocolKind: protocolkind.Responses,
			Primitives:   []CachePrimitive{CachePrimitiveAutoPrefix, CachePrimitiveAffinityKey, CachePrimitiveRetentionHint},
			Notes:        "Ollama v1-compatible surface accepts OpenAI-style cache fields; support is backend-dependent",
		},
		{
			ProviderID:   ProviderSpecChatGPT,
			ProtocolKind: protocolkind.Responses,
			Primitives:   []CachePrimitive{CachePrimitiveAutoPrefix, CachePrimitiveAffinityKey, CachePrimitiveRetentionHint},
			Notes:        "ChatGPT runtime surface is responses_stream; cache controls align with OpenAI-family responses fields",
		},
		{
			ProviderID:   ProviderSpecAnthropic,
			ProtocolKind: protocolkind.Messages,
			Primitives:   []CachePrimitive{CachePrimitiveAutoPrefix, CachePrimitiveBreakpoint, CachePrimitiveRetentionHint},
			Notes:        "Anthropic messages exposes cache_control including explicit breakpoints and TTL classes",
		},
		{
			ProviderID:   ProviderSpecBedrock,
			ProtocolKind: protocolkind.ChatCompletions,
			Primitives:   []CachePrimitive{CachePrimitiveBreakpoint, CachePrimitiveRetentionHint},
			Notes:        "Bedrock converse cachePoint is explicit breakpoint-style and model-dependent",
		},
		{
			ProviderID:   ProviderSpecBedrock,
			ProtocolKind: protocolkind.Completions,
			Primitives:   []CachePrimitive{CachePrimitiveBreakpoint, CachePrimitiveRetentionHint},
			Notes:        "Bedrock invoke paths can expose explicit cache checkpoints for supported models",
		},
		{
			ProviderID:   ProviderSpecOpenRouter,
			ProtocolKind: protocolkind.Responses,
			Primitives:   []CachePrimitive{CachePrimitiveAutoPrefix, CachePrimitiveAffinityKey, CachePrimitiveRetentionHint},
			Notes:        "OpenRouter cache behavior is upstream-route dependent; default Swobu mapping is OpenAI-family auto-prefix path",
		},
	}
}

// SupportsCachePrimitive reports whether one provider route declares a cache
// primitive capability fact.
func SupportsCachePrimitive(provider ProviderID, protocol protocolkind.ProtocolKind, primitive CachePrimitive) bool {
	for _, fact := range CacheCapabilityFacts() {
		if fact.ProviderID != provider || fact.ProtocolKind != protocol {
			continue
		}
		for _, supported := range fact.Primitives {
			if supported == primitive {
				return true
			}
		}
	}
	return false
}
