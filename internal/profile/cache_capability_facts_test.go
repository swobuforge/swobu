package profile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

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
	assertRoute(ProviderSpecBedrock, protocolkind.Responses)
	assertRoute(ProviderSpecAzure, protocolkind.Responses)
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
	if !SupportsCachePrimitive(ProviderSpecBedrock, protocolkind.Responses, CachePrimitiveAffinityKey) {
		t.Fatal("bedrock responses should support affinity_key")
	}
	if SupportsCachePrimitive(ProviderSpecBedrock, protocolkind.Responses, CachePrimitiveBreakpoint) {
		t.Fatal("bedrock responses should not declare breakpoint")
	}
	if !SupportsCachePrimitive(ProviderSpecAzure, protocolkind.Responses, CachePrimitiveAffinityKey) {
		t.Fatal("azure responses should support affinity_key")
	}
}
