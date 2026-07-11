package carrier

import "testing"

func TestDeliveryLegConstants_AreNonEmpty(t *testing.T) {
	legs := []Stage{StageClientRequestIn, StageProviderRequestOut, StageProviderIngressIn, StageClientResponseOut}
	for _, leg := range legs {
		if string(leg) == "" {
			t.Fatal("leg constant must be non-empty")
		}
	}
}
