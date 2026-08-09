package openaifamily

import (
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// ProviderRoutePolicy owns provider route semantics while openaifamily owns shared
// transport/protocol execution mechanics.
type ProviderRoutePolicy interface {
	ProviderID() profile.ProviderID
	AuthStrategy() AuthStrategy
	ToolDiscovery(protocolkind.ProtocolKind) provider.ToolDiscoveryMode
}

type openAIProviderRoutePolicy struct{}
type ollamaProviderRoutePolicy struct{}
type customProviderRoutePolicy struct{}
type openRouterProviderRoutePolicy struct{}
type zaiProviderRoutePolicy struct{}

func (openAIProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenAI
}
func (openAIProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }
func (openAIProviderRoutePolicy) ToolDiscovery(protocol protocolkind.ProtocolKind) provider.ToolDiscoveryMode {
	if protocol == protocolkind.Responses {
		return provider.ToolDiscoveryNative
	}
	return provider.ToolDiscoveryPolyfill
}

func (ollamaProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOllama
}
func (ollamaProviderRoutePolicy) AuthStrategy() AuthStrategy { return NoAuthStrategy() }
func (ollamaProviderRoutePolicy) ToolDiscovery(protocolkind.ProtocolKind) provider.ToolDiscoveryMode {
	return provider.ToolDiscoveryPolyfill
}

func (customProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecCustom
}
func (customProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }
func (customProviderRoutePolicy) ToolDiscovery(protocolkind.ProtocolKind) provider.ToolDiscoveryMode {
	return provider.ToolDiscoveryPolyfill
}

func (openRouterProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecOpenRouter
}
func (openRouterProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }
func (openRouterProviderRoutePolicy) ToolDiscovery(protocolkind.ProtocolKind) provider.ToolDiscoveryMode {
	return provider.ToolDiscoveryPolyfill
}

func (zaiProviderRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecZAI
}
func (zaiProviderRoutePolicy) AuthStrategy() AuthStrategy { return BearerAuthStrategy() }
func (zaiProviderRoutePolicy) ToolDiscovery(protocolkind.ProtocolKind) provider.ToolDiscoveryMode {
	return provider.ToolDiscoveryPolyfill
}

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

// NewZAIPolicy returns the shared Z.AI route policy. Access-specific endpoints
// are derived from routing intent before provider execution reaches this layer.
func NewZAIPolicy() ProviderRoutePolicy { return zaiProviderRoutePolicy{} }
