package messages

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeProviderDocument_InvalidWireCarrierFailsImmediately(t *testing.T) {
	tests := []struct {
		name        string
		doc         carrier.WireDocument
		reasonMatch string
	}{
		{name: "wrong protocol", doc: carrier.NewWireDocument(carrier.StageProviderIngressIn, protocolkind.Responses, "application/json", nil, []byte(`{"ok":true}`), carrier.Meta{}), reasonMatch: "protocol must be"},
		{name: "wrong stage", doc: carrier.NewWireDocument(carrier.StageProviderRequestOut, protocolkind.Messages, "application/json", nil, []byte(`{"ok":true}`), carrier.Meta{}), reasonMatch: "stage must be"},
		{name: "wrong media", doc: carrier.NewWireDocument(carrier.StageProviderIngressIn, protocolkind.Messages, "text/plain", nil, []byte(`{"ok":true}`), carrier.Meta{}), reasonMatch: "media must be"},
		{name: "missing body", doc: carrier.NewWireDocument(carrier.StageProviderIngressIn, protocolkind.Messages, "application/json", nil, nil, carrier.Meta{}), reasonMatch: "body must be configured"},
	}

	codec := legacyProviderDocumentDecoder{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := codec.DecodeProviderDocument(context.Background(), tt.doc, "ex_guard", nil)
			if err == nil {
				t.Fatal("expected decode document guard error, got nil")
			}
			var compatErr canonical.Error
			if !errors.As(err, &compatErr) {
				t.Fatalf("expected canonical.Error, got %T", err)
			}
			if compatErr.Code != canonical.ErrorCodeInternal {
				t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeInternal)
			}
			if compatErr.Message != "messages response wire carrier is invalid" {
				t.Fatalf("error message = %q", compatErr.Message)
			}
			if compatErr.Details == nil || compatErr.Details["wire_document_invariant"] == "" {
				t.Fatalf("missing wire_document_invariant detail: %#v", compatErr.Details)
			}
			if !strings.Contains(compatErr.Details["wire_document_invariant"], tt.reasonMatch) {
				t.Fatalf("wire_document_invariant detail = %q, want substring %q", compatErr.Details["wire_document_invariant"], tt.reasonMatch)
			}
		})
	}
}
