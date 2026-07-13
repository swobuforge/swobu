package azure

import (
	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/profile"
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

// NewPolicy returns the Azure route policy.
func NewPolicy() openaifamily.ProviderRoutePolicy { return providerRoutePolicy{} }
