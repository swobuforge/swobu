package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeAndReencodeRequest_DropsUnknownWireField(t *testing.T) {
	codec := testClientRequestDecoder{}
	req := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"future_vendor_knob":{"enabled":true,"value":123},"stream":true}`)
	got, delivery, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest() error = %v", err)
	}
	if got.Model() != "claude" {
		t.Fatalf("model = %q, want %q", got.Model(), "claude")
	}
	if !delivery.IsStreaming() {
		t.Fatalf("delivery is not streaming")
	}
	providerDocument, err := EncodeCarrier(got, delivery)
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
	if payload["model"] != "claude" || payload["stream"] != true {
		t.Fatalf("known Chat semantics changed: %#v", payload)
	}
}

func TestDecodeRequest_RejectsInvalidStreamOptions(t *testing.T) {
	codec := testClientRequestDecoder{}
	request := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":true}`)
	if _, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: request}); err == nil {
		t.Fatal("expected invalid stream_options error")
	}
}

func TestDecodeRequest_AcceptsStreamOptionsField(t *testing.T) {
	codec := testClientRequestDecoder{}
	req := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true}}`)
	got, delivery, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest() error = %v", err)
	}
	if got.Model() != "claude" {
		t.Fatalf("model = %q, want %q", got.Model(), "claude")
	}
	if !delivery.IsStreaming() {
		t.Fatalf("delivery is not streaming")
	}
	if !delivery.IncludeUsageFrame {
		t.Fatal("include_usage presentation preference was not decoded")
	}
}
