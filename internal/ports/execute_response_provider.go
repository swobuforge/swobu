package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
)

type ProviderRequest = exchange.ProviderRequest
type ExecutionContract = exchange.ExecutionContract
type ProviderResponseStream = exchange.ProviderResponseStream
type ProviderTransportResponse = exchange.ProviderTransportResponse

var NewExecutionContract = exchange.NewExecutionContract
var NewExecutionContractForDeliveries = exchange.NewExecutionContractForDeliveries
var NewEnvelopeStreamingProviderResponseStream = exchange.NewEnvelopeStreamingProviderResponseStream
var CloseProviderResponseStream = exchange.CloseProviderResponseStream

func NewProviderRequest(request canonical.CanonicalRequest, contract ExecutionContract, target exchange.RoutableTarget) ProviderRequest {
	return ProviderRequest{
		Request:      canonical.CloneCanonicalRequest(request),
		ProviderWire: carrier.WireDocument{},
		Contract:     contract,
		Target:       target.Clone(),
	}
}

type ProviderExecutor interface {
	Execute(ctx context.Context, req ProviderRequest) (ProviderTransportResponse, error)
}
