package profile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// CacheCapabilityFact declares cache control primitives for one provider route.
// Facts are intentionally conservative and route-scoped.
type CachePrimitive string

const (
	CachePrimitiveAutoPrefix    CachePrimitive = "auto_prefix"
	CachePrimitiveAffinityKey   CachePrimitive = "affinity_key"
	CachePrimitiveBreakpoint    CachePrimitive = "breakpoint"
	CachePrimitiveCacheRef      CachePrimitive = "cache_ref"
	CachePrimitiveRetentionHint CachePrimitive = "retention_hint"
)

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

func TestCacheCapabilityFacts_CoversActiveProviderSpecs(t *testing.T) {
	facts := CacheCapabilityFacts()
	if len(facts) == 0 {
		t.Fatal("cache capability facts must not be empty")
	}
	assertRoute := func(provider ProviderID, protocol protocolkind.ProtocolKind) {
		t.Helper()
		for _, fact := range facts {
			if fact.ProviderID == provider && fact.ProtocolKind == protocol {
				return
			}
		}
		t.Fatalf("missing cache capability fact for provider=%q protocol=%q", provider, protocol)
	}
	assertRoute(ProviderSpecOpenAI, protocolkind.Responses)
	assertRoute(ProviderSpecOpenAICompatible, protocolkind.Responses)
	assertRoute(ProviderSpecOllama, protocolkind.Responses)
	assertRoute(ProviderSpecChatGPT, protocolkind.Responses)
	assertRoute(ProviderSpecAnthropic, protocolkind.Messages)
	assertRoute(ProviderSpecBedrock, protocolkind.ChatCompletions)
	assertRoute(ProviderSpecBedrock, protocolkind.Completions)
	assertRoute(ProviderSpecOpenRouter, protocolkind.Responses)
}

func TestSupportsCachePrimitive_ConservativeRouteFacts(t *testing.T) {
	if !SupportsCachePrimitive(ProviderSpecOpenAI, protocolkind.Responses, CachePrimitiveAffinityKey) {
		t.Fatal("openai responses should support affinity_key")
	}
	if SupportsCachePrimitive(ProviderSpecOpenAI, protocolkind.Responses, CachePrimitiveBreakpoint) {
		t.Fatal("openai responses should not declare breakpoint")
	}
	if !SupportsCachePrimitive(ProviderSpecAnthropic, protocolkind.Messages, CachePrimitiveBreakpoint) {
		t.Fatal("anthropic messages should support breakpoint")
	}
	if SupportsCachePrimitive(ProviderSpecAnthropic, protocolkind.Messages, CachePrimitiveCacheRef) {
		t.Fatal("anthropic messages should not declare cache_ref")
	}
	if !SupportsCachePrimitive(ProviderSpecBedrock, protocolkind.ChatCompletions, CachePrimitiveBreakpoint) {
		t.Fatal("bedrock converse should support breakpoint")
	}
	if SupportsCachePrimitive(ProviderSpecBedrock, protocolkind.ChatCompletions, CachePrimitiveAffinityKey) {
		t.Fatal("bedrock converse should not declare affinity_key")
	}
}
