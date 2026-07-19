package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeRequest_IgnoresUnknownField(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"unexpected":true}`)
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
}
