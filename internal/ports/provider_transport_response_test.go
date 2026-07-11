package ports

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

func TestProviderIngressValidate_ExactlyOneCarrierRequired(t *testing.T) {
	tests := []struct {
		name    string
		input   ProviderIngress
		wantErr bool
	}{
		{
			name:    "none invalid",
			input:   nil,
			wantErr: true,
		},
		{
			name: "document only valid",
			input: carrier.NewWireDocument(
				carrier.StageProviderIngressIn,
				protocolkind.Responses,
				"application/json",
				nil,
				[]byte(`{"ok":true}`),
				carrier.Meta{},
			),
		},
		{
			name: "stream only valid",
			input: carrier.WireStream{
				Stage:   carrier.StageProviderIngressIn,
				Family:  protocolkind.Responses,
				Framing: carrier.FramingSSE,
				Frames:  carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader("event: response.completed\ndata: {}\n\n"))),
			},
		},
		{
			name: "events only valid",
			input: carrier.CanonicalEventStream{Events: canonical.NewSliceEventReader([]canonical.Event{{
				ExchangeID: "ex",
				Seq:        1,
				Kind:       canonical.EventEnvelopeStart,
				EnvID:      "resp_1",
				Payload: canonical.EnvelopeStartPayload{
					Kind: canonical.EnvResponse,
				},
			}})},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := exchange.ValidateProviderIngress(tc.input)
			if tc.wantErr && err == nil {
				t.Fatal("Validate() expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestProviderCanonicalEventStreamIngress_ExposesReader(t *testing.T) {
	result := carrier.CanonicalEventStream{Events: canonical.NewSliceEventReader([]canonical.Event{{
		ExchangeID: "ex",
		Seq:        1,
		Kind:       canonical.EventEnvelopeStart,
		EnvID:      "resp_1",
		Payload: canonical.EnvelopeStartPayload{
			Kind: canonical.EnvResponse,
		},
	}})}
	reader := result.Events
	ev, err := reader.Next(context.Background())
	if err != nil {
		t.Fatalf("next event: %v", err)
	}
	if ev.ExchangeID != "ex" {
		t.Fatalf("exchange_id=%q", ev.ExchangeID)
	}
}
