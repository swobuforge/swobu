package routing

import (
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func defaultProviderProtocolForProvider(providerSpec string) string {
	if def, ok := providercatalog.DefaultProviderProtocolForSpec(providerSpec); ok {
		return def
	}
	return providercatalog.ProviderProtocolAuto
}
