package chatcompletions

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeRequest_RejectsUnknownField(t *testing.T) {
	codec := ClientRequestDecoder{}
	req := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"unexpected":true}`)
	_, _, err := codec.DecodeClientRequest(carrier.WireDocument{Family: protocolkind.ChatCompletions, Raw: req})
	if err == nil {
		t.Fatal("expected error")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
	if got := compatErr.Details["json_pointer"]; got != "/unexpected" {
		t.Fatalf("json_pointer = %q, want %q", got, "/unexpected")
	}
}
