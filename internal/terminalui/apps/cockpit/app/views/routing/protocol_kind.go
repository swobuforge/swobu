package routing

import (
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func defaultProviderProtocolForProvider(providerSpec string) string {
	return providercatalog.ProviderProtocolAuto
}

func supportedConcreteProviderProtocolsForSpec(providerSpec string) []string {
	all := providercatalog.SupportedProviderProtocolsForSpec(providerSpec)
	out := make([]string, 0, len(all))
	for _, protocol := range all {
		if protocol == providercatalog.ProviderProtocolAuto {
			continue
		}
		out = append(out, protocol)
	}
	return out
}
