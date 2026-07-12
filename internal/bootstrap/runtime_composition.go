package bootstrap

import (
	"context"

	"github.com/swobuforge/swobu/internal/adapters/wire/exchangeruntime"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
)

type providerServicesAdapter struct {
	ingress ports.ProviderIngressResolver
	catalog ports.ProviderModelCatalog
}

func (a providerServicesAdapter) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	return a.ingress.ResolveProviderIngress(ctx, req)
}

func (a providerServicesAdapter) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	return a.catalog.ValidateCredentials(ctx, target)
}

func (a providerServicesAdapter) ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error) {
	return a.catalog.ListModels(ctx, target)
}

// daemonRuntimeComposition is the explicit runtime composition root for the
// daemon live path. It owns the one codec lookup surface and the one provider
// lookup surface without introducing a registry layer.
type daemonRuntimeComposition struct {
	wire      exchangeruntime.RuntimeResolver
	providers providerServicesAdapter
}

func newDaemonRuntimeComposition(wire exchangeruntime.RuntimeResolver, ingress ports.ProviderIngressResolver, catalog ports.ProviderModelCatalog) daemonRuntimeComposition {
	return daemonRuntimeComposition{
		wire: wire,
		providers: providerServicesAdapter{
			ingress: ingress,
			catalog: catalog,
		},
	}
}

func (r daemonRuntimeComposition) ClientCodec(f canonical.ClientFamily) exchange.ClientCodec {
	return r.wire.ClientCodec(f)
}

func (r daemonRuntimeComposition) ProviderRequestDocumentEncoder(kind protocolkind.ProtocolKind) exchange.ProviderRequestDocumentEncoder {
	return r.wire.ProviderRequestDocumentEncoder(kind)
}

func (r daemonRuntimeComposition) ProviderEnvelopeDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) exchange.ProviderEnvelopeDecoder {
	return r.wire.ProviderEnvelopeDecoder(kind, d)
}

func (r daemonRuntimeComposition) ProviderDocumentDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) exchange.ProviderDocumentDecoder {
	return r.wire.ProviderDocumentDecoder(kind, d)
}

func (r daemonRuntimeComposition) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	return r.providers.ResolveProviderIngress(ctx, req)
}

func (r daemonRuntimeComposition) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	return r.providers.ValidateCredentials(ctx, target)
}

func (r daemonRuntimeComposition) ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error) {
	return r.providers.ListModels(ctx, target)
}
