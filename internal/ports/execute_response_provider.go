package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/exchange"
)

// ProviderRequest carries one resolved exchange path and its realized provider
// wire into provider ingress.
type ProviderRequest struct {
	Request         canonical.CanonicalRequest
	RequestDocument carrier.WireDocument
	Contract        exchange.ExecutionContract
	Target          exchange.RoutableTarget
	ExchangeID      string
	ClientFamily    canonical.ClientFamily
	EffectSink      effect.Sink
}

// NewProviderRequest packages one canonical request with its already-realized
// provider wire document for provider ingress.
func NewProviderRequest(request canonical.CanonicalRequest, requestDocument carrier.WireDocument, contract exchange.ExecutionContract, target exchange.RoutableTarget, effectSink ...effect.Sink) ProviderRequest {
	var sink effect.Sink
	if len(effectSink) > 0 {
		sink = effectSink[0]
	}
	return ProviderRequest{
		Request:         canonical.CloneCanonicalRequest(request),
		RequestDocument: requestDocument.Clone(),
		Contract:        contract,
		Target:          target.Clone(),
		EffectSink:      sink,
	}
}

type ProviderIngress = any

type ProviderIngressResolver interface {
	ResolveProviderIngress(ctx context.Context, req ProviderRequest) (ProviderIngress, error)
}
