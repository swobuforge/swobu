package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
)

func TestExecutionContractWithProviderDeliveryOverridesProviderDeliveryOnly(t *testing.T) {
	contract := NewExecutionContract(delivery.BufferedDelivery()).WithProviderDelivery(delivery.StreamingDelivery(delivery.FramingSSE))
	if contract.ClientDelivery != delivery.BufferedDelivery() {
		t.Fatal("client delivery changed")
	}
	if contract.ProviderDelivery != delivery.StreamingDelivery(delivery.FramingSSE) {
		t.Fatal("provider delivery was not replaced")
	}
}

func TestExecutionContractValidate(t *testing.T) {
	valid := NewExecutionContractForDeliveries(delivery.BufferedDelivery(), delivery.StreamingDelivery(delivery.FramingSSE))
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	invalid := NewExecutionContractForDeliveries(delivery.Delivery{Mode: delivery.Buffered, Framing: delivery.FramingSSE}, delivery.BufferedDelivery())
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid client delivery must fail")
	}
	badConversion := valid
	badConversion.ConversionKind = 0
	if err := badConversion.Validate(); err == nil {
		t.Fatal("inconsistent conversion must fail")
	}
}
