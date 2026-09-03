package chatcompletions

import (
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeRequest_IgnoresUnknownField(t *testing.T) {
	codec := testClientRequestDecoder{}
	req := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"unexpected":true,"stream":true}`)
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
