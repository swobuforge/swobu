package openaifamily

import "github.com/swobuforge/swobu/internal/profile"

// ProviderRoutePolicy owns provider route semantics while openaifamily owns shared
// transport/protocol execution mechanics.
type ProviderRoutePolicy interface {
	ProviderID() profile.ProviderID
	AuthStrategy() authStrategy
}

type openAIProviderRoutePolicy struct{}
type ollamaProviderRoutePolicy struct{}
type openAICompatibleProviderRoutePolicy struct{}
type openRouterProviderRoutePolicy struct{}
type azureProviderRoutePolicy struct{}

func (openAIProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenAI
}
func (openAIProviderRoutePolicy) AuthStrategy() authStrategy { return bearerAuthStrategy() }

func (ollamaProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOllama
}
func (ollamaProviderRoutePolicy) AuthStrategy() authStrategy { return noAuthStrategy() }

func (openAICompatibleProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenAICompatible
}
func (openAICompatibleProviderRoutePolicy) AuthStrategy() authStrategy { return bearerAuthStrategy() }

func (azureProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecAzure
}
func (azureProviderRoutePolicy) AuthStrategy() authStrategy { return apiKeyAuthStrategy() }

func (openRouterProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenRouter
}
func (openRouterProviderRoutePolicy) AuthStrategy() authStrategy { return bearerAuthStrategy() }

func NewOpenAIPolicy() ProviderRoutePolicy { return openAIProviderRoutePolicy{} }

func NewOllamaPolicy() ProviderRoutePolicy { return ollamaProviderRoutePolicy{} }

func NewOpenAICompatiblePolicy() ProviderRoutePolicy {
	return openAICompatibleProviderRoutePolicy{}
}

func NewOpenRouterPolicy() ProviderRoutePolicy { return openRouterProviderRoutePolicy{} }

func NewAzurePolicy() ProviderRoutePolicy { return azureProviderRoutePolicy{} }
