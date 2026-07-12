package replay

import (
	"context"

	"github.com/swobuforge/swobu/internal/adapters/wire/exchangeruntime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
)

func withRuntimeRunner(providerIngress func(context.Context, exchange.ProviderRequest) (exchange.ProviderIngress, error)) exchange.Runner {
	return exchange.Runner{
		Runtime: replayRuntime{
			RuntimeResolver: exchangeruntime.NewResolver(),
			providerIngress: providerIngress,
		},
	}
}

type replayRuntime struct {
	exchangeruntime.RuntimeResolver
	providerIngress func(context.Context, exchange.ProviderRequest) (exchange.ProviderIngress, error)
}

func (r replayRuntime) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	if r.providerIngress == nil {
		return nil, canonical.InternalError("replay provider ingress resolver is required")
	}
	return r.providerIngress(ctx, req)
}
