package openaifamily

import "github.com/swobuforge/swobu/internal/profile"

// ProviderRoutePolicy owns provider route semantics while openaifamily owns shared
// transport/protocol execution mechanics.
type ProviderRoutePolicy interface {
	ProviderID() profile.ProviderID
	AuthStrategy() AuthStrategy
}

type openAIProviderRoutePolicy struct{}
type ollamaProviderRoutePolicy struct{}
type customProviderRoutePolicy struct{}
type openRouterProviderRoutePolicy struct{}

func (openAIProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenAI
}
func (openAIProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }

func (ollamaProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOllama
}
func (ollamaProviderRoutePolicy) AuthStrategy() AuthStrategy { return NoAuthStrategy() }

func (customProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecCustom
}
func (customProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }

func (openRouterProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenRouter
}
func (openRouterProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }

// NewOpenAIPolicy returns the OpenAI route policy.
func NewOpenAIPolicy() ProviderRoutePolicy { return openAIProviderRoutePolicy{} }

// NewOllamaPolicy returns the Ollama route policy.
func NewOllamaPolicy() ProviderRoutePolicy { return ollamaProviderRoutePolicy{} }

// NewCustomPolicy returns the custom-endpoint route policy.
func NewCustomPolicy() ProviderRoutePolicy {
	return customProviderRoutePolicy{}
}

// NewOpenRouterPolicy returns the OpenRouter route policy.
func NewOpenRouterPolicy() ProviderRoutePolicy { return openRouterProviderRoutePolicy{} }
