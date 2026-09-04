package messages

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeAndReencodeRequest_DropsUnknownWireField(t *testing.T) {
	codec := testClientRequestDecoder{}
	req := []byte(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"future_vendor_knob":{"enabled":true,"value":123}}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest() error = %v", err)
	}
	if got.Model() != "claude" {
		t.Fatalf("model = %q, want %q", got.Model(), "claude")
	}
	if len(got.Items()) != 1 {
		t.Fatalf("items len = %d, want 1", len(got.Items()))
	}
	providerDocument, err := EncodeCarrier(got, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(providerDocument.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["future_vendor_knob"]; exists {
		t.Fatalf("unknown client field leaked to provider request: %s", providerDocument.RawBytes())
	}
	if payload["model"] != "claude" || payload["max_tokens"] != float64(64) {
		t.Fatalf("known Messages semantics changed: %#v", payload)
	}
}
