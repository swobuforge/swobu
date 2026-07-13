package bootstrap

import (
	"context"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/profile"
)

type providerIngressResolverAdapter struct {
	ingress   exchange.ProviderIngressResolver
	discovery exchange.ProviderModelCatalog
}

func (a providerIngressResolverAdapter) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	portsReq := exchange.ProviderRequest{
		Request:         req.Request,
		RequestDocument: req.RequestDocument,
		Contract:        req.Contract,
		Target:          req.Target,
		ExchangeID:      req.ExchangeID,
		ClientFamily:    req.ClientFamily,
		EffectSink:      req.EffectSink,
	}
	ingress, err := a.ingress.ResolveProviderIngress(ctx, portsReq)
	return ingress, err
}

func (a providerIngressResolverAdapter) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	return a.discovery.ValidateCredentials(ctx, target)
}

func (a providerIngressResolverAdapter) ListDeployments(ctx context.Context, target exchange.RoutableTarget) ([]profile.ProviderDeploymentRecord, error) {
	return a.discovery.ListDeployments(ctx, target)
}

// daemonProviderModelCatalogComposition is the explicit runtime composition root for
// the daemon live path. It owns the one codec lookup surface and the one
// provider lookup surface without introducing a registry layer.
type daemonProviderModelCatalogComposition struct {
	wire      codecresolver.RuntimeCodecResolver
	providers providerIngressResolverAdapter
}

func newDaemonProviderModelCatalogComposition(wire codecresolver.RuntimeCodecResolver, ingress exchange.ProviderIngressResolver, discovery exchange.ProviderModelCatalog) daemonProviderModelCatalogComposition {
	return daemonProviderModelCatalogComposition{
		wire: wire,
		providers: providerIngressResolverAdapter{
			ingress:   ingress,
			discovery: discovery,
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

func (r daemonProviderModelCatalogComposition) ListDeployments(ctx context.Context, target exchange.RoutableTarget) ([]profile.ProviderDeploymentRecord, error) {
	return r.providers.ListDeployments(ctx, target)
}
