package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
)

type ProviderRequest = exchange.ProviderRequest

type ProviderIngress = exchange.ProviderIngress

// NewProviderRequest packages one canonical request with its already-realized
// provider wire document for provider ingress.
func NewProviderRequest(request canonical.CanonicalRequest, requestDocument carrier.WireDocument, contract exchange.ExecutionContract, target exchange.RoutableTarget) ProviderRequest {
	return ProviderRequest{
		Request:         canonical.CloneCanonicalRequest(request),
		RequestDocument: requestDocument.Clone(),
		Contract:        contract,
		Target:          target.Clone(),
	}
}

type ProviderIngressResolver interface {
	ResolveProviderIngress(ctx context.Context, req ProviderRequest) (ProviderIngress, error)
}
