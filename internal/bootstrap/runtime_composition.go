package bootstrap

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/provider"
)

// daemonProviderModelCatalogComposition is the explicit runtime composition root for
// the daemon live path. It owns the one codec lookup surface and the one
// provider lookup surface without introducing a registry layer.
type daemonProviderModelCatalogComposition struct {
	wire      codecresolver.RuntimeCodecResolver
	providers provider.BackendResolver
	discovery provider.Discovery
}

func newDaemonProviderModelCatalogComposition(wire codecresolver.RuntimeCodecResolver, backends provider.BackendResolver, discovery provider.Discovery) daemonProviderModelCatalogComposition {
	return daemonProviderModelCatalogComposition{
		wire:      wire,
		providers: backends,
		discovery: discovery,
	}
}

func (r daemonProviderModelCatalogComposition) ClientCodec(f canonical.ClientFamily) exchange.ClientCodec {
	return r.wire.ClientCodec(f)
}

func (r daemonProviderModelCatalogComposition) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return r.providers.ResolveBackend(target)
}

func (r daemonProviderModelCatalogComposition) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	return r.discovery.ProbeTarget(ctx, target)
}
