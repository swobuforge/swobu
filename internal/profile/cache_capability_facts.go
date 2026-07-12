package profile

import "github.com/swobuforge/swobu/internal/domain/protocolkind"

// CachePrimitive declares one cache-control capability primitive for a route.
type CachePrimitive string

const (
	CachePrimitiveAutoPrefix    CachePrimitive = "auto_prefix"
	CachePrimitiveAffinityKey   CachePrimitive = "affinity_key"
	CachePrimitiveBreakpoint    CachePrimitive = "breakpoint"
	CachePrimitiveCacheRef      CachePrimitive = "cache_ref"
	CachePrimitiveRetentionHint CachePrimitive = "retention_hint"
)

// CacheCapabilityFact declares cache-control primitives for one provider route.
// Facts are intentionally conservative and route-scoped.
type CacheCapabilityFact struct {
	ProviderID   ProviderID
	ProtocolKind protocolkind.ProtocolKind
	Primitives   []CachePrimitive
	Notes        string
}

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
			Notes:        "OpenAI-style routes are treated as OpenAI-family cache controls",
		},
		{
			ProviderID:   ProviderSpecOllama,
			ProtocolKind: protocolkind.Responses,
			Primitives:   []CachePrimitive{CachePrimitiveAutoPrefix, CachePrimitiveAffinityKey, CachePrimitiveRetentionHint},
			Notes:        "Ollama v1-style surface accepts OpenAI-style cache fields; support is backend-dependent",
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
			ProtocolKind: protocolkind.Responses,
			Primitives:   []CachePrimitive{CachePrimitiveAutoPrefix, CachePrimitiveAffinityKey, CachePrimitiveRetentionHint},
			Notes:        "Bedrock Mantle responses follows OpenAI-style cache controls where supported",
		},
		{
			ProviderID:   ProviderSpecAzure,
			ProtocolKind: protocolkind.Responses,
			Primitives:   []CachePrimitive{CachePrimitiveAutoPrefix, CachePrimitiveAffinityKey, CachePrimitiveRetentionHint},
			Notes:        "Azure OpenAI v1 responses follows OpenAI-family cache controls where supported",
		},
		{
			ProviderID:   ProviderSpecOpenRouter,
			ProtocolKind: protocolkind.Responses,
			Primitives:   []CachePrimitive{CachePrimitiveAutoPrefix, CachePrimitiveAffinityKey, CachePrimitiveRetentionHint},
			Notes:        "OpenRouter cache behavior is provider-route dependent; default Swobu mapping is OpenAI-family auto-prefix path",
		},
	}
}

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
