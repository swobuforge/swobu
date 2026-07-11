package exchange

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type EndpointReader interface {
	GetEndpoint(ctx context.Context, name endpointintent.EndpointName) (endpointintent.Endpoint, error)
}

type ProviderIngressResolver interface {
	ResolveProviderIngress(ctx context.Context, req ProviderRequest) (ProviderIngress, error)
}

// TODO god object
type ProviderRequest struct {
	Request         canonical.CanonicalRequest
	RequestDocument carrier.WireDocument
	Contract        ExecutionContract
	Target          RoutableTarget
	ExchangeID      string
	ClientFamily    canonical.ClientFamily
}

func NewProviderRequest(
	exchangeID string,
	clientFamily canonical.ClientFamily,
	request canonical.CanonicalRequest,
	wireRequest carrier.WireDocument,
	contract ExecutionContract,
	target RoutableTarget,
) ProviderRequest {
	return ProviderRequest{
		ExchangeID:      exchangeID,
		ClientFamily:    clientFamily,
		Request:         canonical.CloneCanonicalRequest(request),
		RequestDocument: wireRequest.Clone(),
		Contract:        contract,
		Target:          target.Clone(),
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

func (c ExecutionContract) Validate() error {
	if err := c.ClientDelivery.Validate(); err != nil {
		return fmt.Errorf("client delivery is invalid")
	}
	if err := c.ProviderDelivery.Validate(); err != nil {
		return fmt.Errorf("provider delivery is invalid")
	}
	if want := delivery.DeriveConversion(c.ClientDelivery, c.ProviderDelivery); want != c.ConversionKind {
		return fmt.Errorf("delivery conversion kind is inconsistent with client/provider delivery")
	}
	return nil
}

type ProviderIngress any

func ValidateProviderIngress(ingress ProviderIngress) error {
	switch in := ingress.(type) {
	case carrier.WireDocument:
		if in.IsEmpty() {
			return fmt.Errorf("provider ingress wire document must not be empty")
		}
		if in.Stage != carrier.StageProviderIngressIn {
			return fmt.Errorf("provider ingress wire document must use %q carrier stage", carrier.StageProviderIngressIn)
		}
		return nil
	case carrier.WireStream:
		if in.Frames == nil {
			return fmt.Errorf("provider ingress wire stream is required")
		}
		if in.Stage != carrier.StageProviderIngressIn {
			return fmt.Errorf("provider ingress wire stream must use %q carrier stage", carrier.StageProviderIngressIn)
		}
		return nil
	case carrier.CanonicalEventStream:
		if in.Events == nil {
			return fmt.Errorf("provider ingress canonical event stream is required")
		}
		return nil
	case nil:
		return fmt.Errorf("provider ingress is required")
	default:
		return fmt.Errorf("provider ingress carrier %T is unsupported", ingress)
	}
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

func NewRoutableTarget(backendRef string, targetSpec string, baseURL string, credentialRef string, protocolKind protocolkind.ProtocolKind, authKind string, selectedFrame string, targetProtocol ...string) RoutableTarget {
	resolvedProviderProtocol := ""
	if len(targetProtocol) > 0 {
		resolvedProviderProtocol = targetProtocol[0]
	}
	return RoutableTarget{BackendRef: backendRef, ProviderSpec: targetSpec, BaseURL: baseURL, CredentialRef: credentialRef, ProtocolKind: protocolKind, AuthKind: authKind, SelectedFrame: selectedFrame, ProviderProtocol: resolvedProviderProtocol}
}

func (t RoutableTarget) Clone() RoutableTarget {
	return NewRoutableTarget(t.BackendRef, t.ProviderSpec, t.BaseURL, t.CredentialRef, t.ProtocolKind, t.AuthKind, t.SelectedFrame, t.ProviderProtocol)
}

func (t RoutableTarget) ProviderID() string { return t.ProviderSpec }
