package routetarget

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

// ResolvedRoutableTarget is the execution-ready routable target resolved from
// one configured provider set.
type ResolvedRoutableTarget struct {
	EndpointName   endpointintent.EndpointName
	ProviderConfig endpointintent.ProviderConfig
	RouteProfile   providercatalog.RouteProfile
}

func ResolveRoutableTarget(endpoint endpointintent.Endpoint) (ResolvedRoutableTarget, error) {
	providerConfig := endpoint.SelectedProviderConfig()
	if providerConfig.Ref().String() == "" {
		return ResolvedRoutableTarget{}, canonical.BadEndpoint("selected provider config is missing")
	}
	routeProfile, ok := providercatalog.ResolveRouteProfile(
		providerConfig.ProviderSpec().String(),
		providerConfig.BaseURL(),
		providerConfig.CredentialRef(),
	)
	if !ok {
		return ResolvedRoutableTarget{}, canonical.BadEndpoint("selected provider route is unsupported")
	}
	return ResolvedRoutableTarget{
		EndpointName:   endpoint.Name(),
		ProviderConfig: providerConfig,
		RouteProfile:   routeProfile,
	}, nil
}
