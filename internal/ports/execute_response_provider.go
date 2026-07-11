package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
)

type ProviderRequest = exchange.ProviderRequest

// ExecutionContract aliases the exchange-owned adapter-edge delivery contract.
// Field documentation lives on exchange.ExecutionContract so the seam has one
// canonical definition.
type ExecutionContract = exchange.ExecutionContract
type ProviderIngress = exchange.ProviderIngress

var NewExecutionContract = exchange.NewExecutionContract
var NewExecutionContractForDeliveries = exchange.NewExecutionContractForDeliveries

func NewProviderRequest(request canonical.CanonicalRequest, contract ExecutionContract, target exchange.RoutableTarget) ProviderRequest {
	return ProviderRequest{
		Request:         canonical.CloneCanonicalRequest(request),
		RequestDocument: carrier.WireDocument{},
		Contract:        contract,
		Target:          target.Clone(),
	}
}

type ProviderIngressResolver interface {
	ResolveProviderIngress(ctx context.Context, req ProviderRequest) (ProviderIngress, error)
}
