package provider

import (
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestProviderContractsHaveNoContinuationSidecar(t *testing.T) {
	for name, typ := range map[string]reflect.Type{
		"Request":         reflect.TypeOf(Request{}),
		"Backend":         reflect.TypeOf(Backend{}),
		"DecodedResponse": reflect.TypeOf(DecodedResponse{}),
	} {
		for _, field := range []string{"Continuation", "CaptureContinuation", "NativeContinuation"} {
			if _, ok := typ.FieldByName(field); ok {
				t.Fatalf("%s still exposes %s", name, field)
			}
		}
	}
}

func TestRequestDeliveryIsProviderFacingWireIntent(t *testing.T) {
	req := Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: "m"}),
		Delivery:  delivery.StreamingDelivery(delivery.FramingSSE),
	}
	if req.Delivery != delivery.StreamingDelivery(delivery.FramingSSE) {
		t.Fatalf("delivery = %#v", req.Delivery)
	}
}
