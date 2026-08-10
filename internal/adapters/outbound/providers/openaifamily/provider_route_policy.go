package openaifamily

import "github.com/swobuforge/swobu/internal/profile"

// ProviderRoutePolicy owns provider route semantics while openaifamily owns shared
// transport/protocol execution mechanics.
type ProviderRoutePolicy interface {
	ProviderID() profile.ProviderID
	AuthStrategy() AuthStrategy
	ModelCatalogDialect() ModelCatalogDialect
}

// ModelCatalogDialect selects the provider-owned model-list wire contract.
// Inference protocol selection remains independent of this operator-side fact.
type ModelCatalogDialect uint8

const (
	ModelCatalogOpenAI ModelCatalogDialect = iota + 1
	ModelCatalogLMStudioV1
)

type openAIProviderRoutePolicy struct{}
type ollamaProviderRoutePolicy struct{}
type lmStudioProviderRoutePolicy struct{}
type vllmProviderRoutePolicy struct{}
type customProviderRoutePolicy struct{}
type openRouterProviderRoutePolicy struct{}
type zaiProviderRoutePolicy struct{}

func (openAIProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenAI
}
func (openAIProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }
func (openAIProviderRoutePolicy) ModelCatalogDialect() ModelCatalogDialect {
	return ModelCatalogOpenAI
}

func (ollamaProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOllama
}
func (ollamaProviderRoutePolicy) AuthStrategy() AuthStrategy { return NoAuthStrategy() }
func (ollamaProviderRoutePolicy) ModelCatalogDialect() ModelCatalogDialect {
	return ModelCatalogOpenAI
}

func (lmStudioProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecLMStudio
}
func (lmStudioProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }
func (lmStudioProviderRoutePolicy) ModelCatalogDialect() ModelCatalogDialect {
	return ModelCatalogLMStudioV1
}

func (vllmProviderRoutePolicy) ProviderID() profile.ProviderID { return profile.ProviderSpecVLLM }
func (vllmProviderRoutePolicy) AuthStrategy() AuthStrategy     { return BearerAuthStrategy() }
func (vllmProviderRoutePolicy) ModelCatalogDialect() ModelCatalogDialect {
	return ModelCatalogOpenAI
}

func (customProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecCustom
}
func (customProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }
func (customProviderRoutePolicy) ModelCatalogDialect() ModelCatalogDialect {
	return ModelCatalogOpenAI
}

func (openRouterProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenRouter
}
func (openRouterProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }
func (openRouterProviderRoutePolicy) ModelCatalogDialect() ModelCatalogDialect {
	return ModelCatalogOpenAI
}

func (zaiProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecZAI
}
func (zaiProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }
func (zaiProviderRoutePolicy) ModelCatalogDialect() ModelCatalogDialect {
	return ModelCatalogOpenAI
}

// NewOpenAIPolicy returns the OpenAI route policy.
func NewOpenAIPolicy() ProviderRoutePolicy { return openAIProviderRoutePolicy{} }

// NewOllamaPolicy returns the Ollama route policy.
func NewOllamaPolicy() ProviderRoutePolicy { return ollamaProviderRoutePolicy{} }

// NewLMStudioPolicy returns the LM Studio route policy.
func NewLMStudioPolicy() ProviderRoutePolicy { return lmStudioProviderRoutePolicy{} }

// NewVLLMPolicy returns the vLLM standard-serving route policy.
func NewVLLMPolicy() ProviderRoutePolicy { return vllmProviderRoutePolicy{} }

// NewCustomPolicy returns the custom-endpoint route policy.
func NewCustomPolicy() ProviderRoutePolicy {
	return customProviderRoutePolicy{}
}

// NewOpenRouterPolicy returns the OpenRouter route policy.
func NewOpenRouterPolicy() ProviderRoutePolicy { return openRouterProviderRoutePolicy{} }

// NewZAIPolicy returns the shared Z.AI route policy. Access-specific endpoints
// are derived from routing intent before provider execution reaches this layer.
func NewZAIPolicy() ProviderRoutePolicy { return zaiProviderRoutePolicy{} }
