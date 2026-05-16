package requestpath

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/domain/routetarget"
	"github.com/swobuforge/swobu/internal/ports"
)

func routableTargetFromProviderConfig(endpointName endpointintent.EndpointName, providerConfig endpointintent.ProviderConfig) (ports.RoutableTarget, error) {
	routeProfile, ok := providercatalog.ResolveRouteProfile(
		providerConfig.ProviderSpec().String(),
		providerConfig.BaseURL(),
		providerConfig.CredentialRef(),
	)
	if !ok {
		return ports.RoutableTarget{}, canonical.BadEndpoint("selected provider route is unsupported")
	}
	resolved := routetarget.ResolvedRoutableTarget{
		EndpointName:   endpointName,
		ProviderConfig: providerConfig,
		RouteProfile:   routeProfile,
	}
	return routableTargetFromResolved(resolved), nil
}

func routableTargetFromResolved(resolved routetarget.ResolvedRoutableTarget) ports.RoutableTarget {
	providerConfig := resolved.ProviderConfig
	return ports.NewRoutableTarget(
		providerConfig.Ref().String(),
		providerConfig.ProviderSpec().String(),
		providerConfig.BaseURL(),
		providerConfig.CredentialRef(),
		providerConfig.ProtocolKind(),
		string(resolved.RouteProfile.AuthKind),
		providerConfig.SelectedFrame(),
	)
}
