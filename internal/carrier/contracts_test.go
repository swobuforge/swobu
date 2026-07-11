package carrier

import "testing"

func TestDeliveryLegConstants_AreNonEmpty(t *testing.T) {
	legs := []Leg{LegClientRequestIn, LegProviderRequestOut, LegProviderResponseIn, LegClientResponseOut}
	for _, leg := range legs {
		if string(leg) == "" {
			t.Fatal("leg constant must be non-empty")
		}
	}
}
