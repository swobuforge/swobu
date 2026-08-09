package azure

import (
	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

type providerRoutePolicy struct{}

func (providerRoutePolicy) ProviderID() profile.ProviderID {
	return profile.ProviderSpecAzure
}

func (providerRoutePolicy) AuthStrategy() openaifamily.AuthStrategy {
	return openaifamily.AuthStrategy{
		Header: openaifamily.AuthHeaderAPIKey,
		Style:  openaifamily.AuthStyleValue,
	}
}

func (providerRoutePolicy) ToolDiscovery(protocol protocolkind.ProtocolKind) provider.ToolDiscoveryMode {
	if protocol == protocolkind.Responses {
		return provider.ToolDiscoveryNative
	}
	return provider.ToolDiscoveryPolyfill
}

func (providerRoutePolicy) ModelCatalogDialect() openaifamily.ModelCatalogDialect {
	return openaifamily.ModelCatalogOpenAI
}

// NewPolicy returns the Azure route policy.
func NewPolicy() openaifamily.ProviderRoutePolicy { return providerRoutePolicy{} }
