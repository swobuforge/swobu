package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestRealizeProviderRequestCarrier(t *testing.T) {
	t.Parallel()

	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     "m",
		InputText: "hi",
	})
	doc, err := RealizeProviderRequestCarrier(req, protocolkind.Responses, delivery.BufferedDelivery(), "messages unsupported")
	if err != nil {
		t.Fatalf("RealizeProviderRequestCarrier() error = %v", err)
	}
	if len(doc.Raw) == 0 {
		t.Fatal("raw provider request must not be empty")
	}
}
