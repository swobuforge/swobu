package exchange

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type EndpointReader interface {
	GetEndpoint(ctx context.Context, name endpointintent.EndpointName) (endpointintent.Endpoint, error)
}

type ProviderExecutor interface {
	Execute(ctx context.Context, req ProviderRequest) (ProviderTransportResponse, error)
}

type ProviderRequest struct {
	Request      canonical.CanonicalRequest
	ProviderWire carrier.WireDocument
	Contract     ExecutionContract
	Target       RoutableTarget
	ExchangeID   string
	ClientFamily canonical.IngressFamily
}

func NewProviderRequest(
	exchangeID string,
	clientFamily canonical.IngressFamily,
	request canonical.CanonicalRequest,
	wireRequest carrier.WireDocument,
	contract ExecutionContract,
	target RoutableTarget,
) ProviderRequest {
	return ProviderRequest{
		ExchangeID:   exchangeID,
		ClientFamily: clientFamily,
		Request:      canonical.CloneCanonicalRequest(request),
		ProviderWire: carrier.WireDocument{
			Leg:    wireRequest.Leg,
			Family: wireRequest.Family,
			Media:  wireRequest.Media,
			Header: wireRequest.Header,
			Raw:    append([]byte(nil), wireRequest.Raw...),
			Meta:   wireRequest.Meta,
		},
		Contract: contract,
		Target:   target.Clone(),
	}
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
	return ExecutionContract{
		ClientDelivery:   clientDelivery,
		ProviderDelivery: providerDelivery,
		ConversionKind:   delivery.DeriveConversion(clientDelivery, providerDelivery),
	}
}

func (c ExecutionContract) WithProviderDelivery(next delivery.Delivery) ExecutionContract {
	c.ProviderDelivery = next
	c.ConversionKind = delivery.DeriveConversion(c.ClientDelivery, c.ProviderDelivery)
	return c
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

type RoutableTarget struct {
	BackendRef       string
	ProviderSpec     string
	BaseURL          string
	CredentialRef    string
	ProtocolKind     protocolkind.ProtocolKind
	AuthKind         string
	SelectedFrame    string
	ProviderProtocol string
}

func NewRoutableTarget(
	backendRef string,
	providerSpec string,
	baseURL string,
	credentialRef string,
	protocolKind protocolkind.ProtocolKind,
	authKind string,
	selectedFrame string,
	providerProtocol ...string,
) RoutableTarget {
	resolvedProviderProtocol := ""
	if len(providerProtocol) > 0 {
		resolvedProviderProtocol = providerProtocol[0]
	}
	return RoutableTarget{
		BackendRef:       backendRef,
		ProviderSpec:     providerSpec,
		BaseURL:          baseURL,
		CredentialRef:    credentialRef,
		ProtocolKind:     protocolKind,
		AuthKind:         authKind,
		SelectedFrame:    selectedFrame,
		ProviderProtocol: resolvedProviderProtocol,
	}
}

func (t RoutableTarget) Clone() RoutableTarget {
	return NewRoutableTarget(
		t.BackendRef,
		t.ProviderSpec,
		t.BaseURL,
		t.CredentialRef,
		t.ProtocolKind,
		t.AuthKind,
		t.SelectedFrame,
		t.ProviderProtocol,
	)
}

func (t RoutableTarget) ProviderID() string {
	return t.ProviderSpec
}
