package bootstrap

import (
	"context"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
)

type ExchangeProviderIngressResolverAdapter struct {
	next ports.ProviderIngressResolver
}

func newExchangeProviderIngressResolverAdapter(next ports.ProviderIngressResolver) ExchangeProviderIngressResolverAdapter {
	return ExchangeProviderIngressResolverAdapter{next: next}
}

func (a ExchangeProviderIngressResolverAdapter) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	portsReq := ports.NewProviderRequest(
		req.Request,
		ports.NewExecutionContractForDeliveries(req.Contract.ClientDelivery, req.Contract.ProviderDelivery),
		exchange.NewRoutableTarget(
			req.Target.BackendRef,
			req.Target.ProviderSpec,
			req.Target.BaseURL,
			req.Target.CredentialRef,
			req.Target.ProtocolKind,
			req.Target.AuthKind,
			req.Target.SelectedFrame,
			req.Target.ProviderProtocol,
		),
	)
	portsReq.RequestDocument = req.RequestDocument
	portsResp, err := a.next.ResolveProviderIngress(ctx, portsReq)
	if err != nil {
		return nil, err
	}
	return portsResp, nil
}
