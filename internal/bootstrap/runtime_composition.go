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

type providerIngressResolverAdapter struct {
	ingress ports.ProviderIngressResolver
	catalog ports.ProviderModelCatalog
}

func (a providerIngressResolverAdapter) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	portsReq := ports.ProviderRequest{
		Request:         req.Request,
		RequestDocument: req.RequestDocument,
		Contract:        req.Contract,
		Target:          req.Target,
		ExchangeID:      req.ExchangeID,
		ClientFamily:    req.ClientFamily,
	}
	ingress, err := a.ingress.ResolveProviderIngress(ctx, portsReq)
	return ingress, err
}

func (a providerIngressResolverAdapter) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	return a.catalog.ValidateCredentials(ctx, target)
}

func (a providerIngressResolverAdapter) ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error) {
	return a.catalog.ListModels(ctx, target)
}

// daemonProviderModelCatalogComposition is the explicit runtime composition root for the
// daemon live path. It owns the one codec lookup surface and the one provider
// lookup surface without introducing a registry layer.
type daemonProviderModelCatalogComposition struct {
	wire      exchangeruntime.RuntimeResolver
	providers providerIngressResolverAdapter
}

func newDaemonProviderModelCatalogComposition(wire exchangeruntime.RuntimeResolver, ingress ports.ProviderIngressResolver, catalog ports.ProviderModelCatalog) daemonProviderModelCatalogComposition {
	return daemonProviderModelCatalogComposition{
		wire: wire,
		providers: providerIngressResolverAdapter{
			ingress: ingress,
			catalog: catalog,
		},
	}
}

func (r daemonProviderModelCatalogComposition) ClientCodec(f canonical.ClientFamily) exchange.ClientCodec {
	return r.wire.ClientCodec(f)
}

func (r daemonProviderModelCatalogComposition) ProviderRequestDocumentEncoder(kind protocolkind.ProtocolKind) exchange.ProviderRequestDocumentEncoder {
	return r.wire.ProviderRequestDocumentEncoder(kind)
}

func (r daemonProviderModelCatalogComposition) ProviderEnvelopeDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) exchange.ProviderEnvelopeDecoder {
	return r.wire.ProviderEnvelopeDecoder(kind, d)
}

func (r daemonProviderModelCatalogComposition) ProviderDocumentDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) exchange.ProviderDocumentDecoder {
	return r.wire.ProviderDocumentDecoder(kind, d)
}

func (r daemonProviderModelCatalogComposition) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	return r.providers.ResolveProviderIngress(ctx, req)
}

func (r daemonProviderModelCatalogComposition) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	return r.providers.ValidateCredentials(ctx, target)
}

func (r daemonProviderModelCatalogComposition) ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error) {
	return r.providers.ListModels(ctx, target)
}
