package exchange

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/delivery"
)

// ExecutionContract records client- and provider-facing delivery truth plus
// the conversion required between them. Provider codecs receive only
// ProviderDelivery through provider.Request.
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
