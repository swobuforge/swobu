package completions

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeProviderEnvelope_InvalidWireCarrierFailsImmediately(t *testing.T) {
	tests := []struct {
		name        string
		wire        carrier.WireStream
		reasonMatch string
	}{
		{name: "wrong protocol", wire: carrier.WireStream{Family: protocolkind.ChatCompletions, Frames: carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(""))), Framing: carrier.FramingSSE}, reasonMatch: "protocol must be"},
		{name: "wrong framing", wire: carrier.WireStream{Family: protocolkind.Completions, Frames: carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(""))), Framing: carrier.FramingNDJSON}, reasonMatch: "framing must be"},
		{name: "missing frames", wire: carrier.WireStream{Family: protocolkind.Completions, Framing: carrier.FramingSSE}, reasonMatch: "frames must be configured"},
	}

	codec := ProviderEnvelopeDecoder{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := codec.DecodeProviderEnvelope(tt.wire, "ex_guard")
			_, err := reader.Next(context.Background())
			if err == nil {
				t.Fatal("expected decode stream guard error, got nil")
			}
			var compatErr canonical.Error
			if !errors.As(err, &compatErr) {
				t.Fatalf("expected canonical.Error, got %T", err)
			}
			if compatErr.Code != canonical.ErrorCodeInternal {
				t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeInternal)
			}
			if compatErr.Message != "completions stream wire carrier is invalid" {
				t.Fatalf("error message = %q", compatErr.Message)
			}
			if strings.TrimSpace(compatErr.Details["wire_stream_invariant"]) == "" {
				t.Fatalf("missing wire_stream_invariant detail: %#v", compatErr.Details)
			}
			if !strings.Contains(compatErr.Details["wire_stream_invariant"], tt.reasonMatch) {
				t.Fatalf("wire_stream_invariant detail = %q, want substring %q", compatErr.Details["wire_stream_invariant"], tt.reasonMatch)
			}
		})
	}
}
