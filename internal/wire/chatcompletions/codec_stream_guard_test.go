package chatcompletions

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeProviderEnvelope_InvalidWireCarrierFailsImmediately(t *testing.T) {
	tests := []struct {
		name        string
		wire        carrier.ByteStream
		reasonMatch string
	}{
		{name: "missing body", wire: carrier.ByteStream{MediaType: "text/event-stream"}, reasonMatch: "body must be configured"},
	}

	codec := ProviderEnvelopeDecoder{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, decodeErr := codec.DecodeProviderEnvelope(canonical.CanonicalRequest{}, tt.wire, "ex_guard")
			if decodeErr == nil {
				t.Fatal("expected decode stream guard error, got nil")
			}
			var compatErr canonical.Error
			if !errors.As(decodeErr, &compatErr) {
				t.Fatalf("expected canonical.Error, got %T", decodeErr)
			}
			if compatErr.Code != canonical.ErrorCodeInternal {
				t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeInternal)
			}
			if compatErr.Message != "chat completions stream wire carrier is invalid" {
				t.Fatalf("error message = %q", compatErr.Message)
			}
			if strings.TrimSpace(compatErr.Details["wire_stream_invariant"]) == "" { // swobu:io-string source=domain
				t.Fatalf("missing wire_stream_invariant detail: %#v", compatErr.Details)
			}
			if !strings.Contains(compatErr.Details["wire_stream_invariant"], tt.reasonMatch) {
				t.Fatalf("wire_stream_invariant detail = %q, want substring %q", compatErr.Details["wire_stream_invariant"], tt.reasonMatch)
			}
		})
	}
}
