package ports

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type ProviderRequest struct {
	Request  canonical.CanonicalRequest
	Contract ExecutionContract
	Target   RoutableTarget
}

type ExecutionContract struct {
	ClientDelivery   delivery.Delivery
	ProviderDelivery delivery.Delivery
	ConversionKind   delivery.Conversion
}

func NewExecutionContract(client delivery.Delivery) ExecutionContract {
	return NewExecutionContractForDeliveries(client, client)
}
func NewExecutionContractForDeliveries(clientDelivery delivery.Delivery, providerDelivery delivery.Delivery) ExecutionContract {
	return ExecutionContract{ClientDelivery: clientDelivery, ProviderDelivery: providerDelivery, ConversionKind: delivery.DeriveConversion(clientDelivery, providerDelivery)}
}
func (c ExecutionContract) WithProviderDelivery(next delivery.Delivery) ExecutionContract {
	c.ProviderDelivery = next
	c.ConversionKind = delivery.DeriveConversion(c.ClientDelivery, c.ProviderDelivery)
	return c
}

func NewProviderRequest(request canonical.CanonicalRequest, contract ExecutionContract, target RoutableTarget) ProviderRequest {
	return ProviderRequest{Request: canonical.CloneCanonicalRequest(request), Contract: contract, Target: target.Clone()}
}

type ProviderResponseStream struct {
	envelope canonical.EventReader
}

func NewEnvelopeStreamingProviderResponseStream(envelope canonical.EventReader) ProviderResponseStream {
	return ProviderResponseStream{envelope: envelope}
}
func (r ProviderResponseStream) EnvelopeStream() canonical.EventReader { return r.envelope }
func CloseProviderResponseStream(r ProviderResponseStream) error {
	if r.envelope != nil {
		return r.envelope.Close(context.Background())
	}
	return nil
}

type ProviderExecutor interface {
	Execute(ctx context.Context, req ProviderRequest) (ProviderTransportResponse, error)
}

type ProviderTransportResponse struct {
	Header   http.Header
	Document []byte
	Stream   io.ReadCloser
	Envelope canonical.EventReader
}

func (r ProviderTransportResponse) Validate() error {
	hasEnvelope := r.Envelope != nil
	hasStream := r.Stream != nil
	hasDocument := len(r.Document) > 0
	count := 0
	if hasEnvelope {
		count++
	}
	if hasStream {
		count++
	}
	if hasDocument {
		count++
	}
	if count != 1 {
		return fmt.Errorf("provider transport response must contain exactly one success carrier")
	}
	return nil
}
