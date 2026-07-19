package provider

import (
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestTargetSnapshotBindsNativeContinuationToItsVersion(t *testing.T) {
	target := NewTargetSnapshot("tgt-1", "openai", "https://api.openai.com/v1", "credential-a", "responses", "")
	native := target.NativeContinuation("resp_1")
	if native == nil || native.TargetID != target.TargetID || native.TargetVersion != target.TargetVersion || native.ID != "resp_1" {
		t.Fatalf("native continuation = %#v", native)
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
