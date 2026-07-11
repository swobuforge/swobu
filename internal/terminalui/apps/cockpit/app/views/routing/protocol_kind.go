package routing

import (
	"github.com/swobuforge/swobu/internal/profile"
)

func defaultProviderProtocolForProvider(providerSpec string) string {
	return profile.ProviderProtocolAuto
}

func supportedConcreteProviderProtocolsForSpec(providerSpec string) []string {
	all := profile.SupportedProviderProtocolsForSpec(providerSpec)
	out := make([]string, 0, len(all))
	for _, protocol := range all {
		if protocol == profile.ProviderProtocolAuto {
			continue
		}
		out = append(out, protocol)
	}
	return out
}
