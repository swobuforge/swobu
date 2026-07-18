package exchange

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
)

func TestNewProviderRequest_ClonesCanonicalRequestAndTargetInputs(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: "m", Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")}})
	target := NewRoutableTarget("backend-a", "openai_"+"com"+"patible", "http://localhost:8080/v1", "cred-1", "chat_completions", "", "")
	wireRequest := carrier.NewCarrierDocument(carrier.StageProviderRequestOut, protocolkind.Responses, "application/json", nil, []byte(`{"request":true}`), carrier.Meta{})
	req := NewProviderRequest("ex-1", protocolkind.Responses, request, wireRequest, NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)), target)
	if req.Contract.ClientDelivery != delivery.StreamingDelivery(delivery.FramingSSE) || req.Contract.ProviderDelivery != delivery.StreamingDelivery(delivery.FramingSSE) {
		t.Fatalf("delivery clone mismatch")
	}
	if !bytes.Equal(req.RequestDocument.RawBytes(), wireRequest.RawBytes()) {
		t.Fatalf("request document clone mismatch")
	}
}

func TestExecutionContract_WithProviderDelivery_OverridesProviderDeliveryOnly(t *testing.T) {
	contract := NewExecutionContract(delivery.BufferedDelivery()).WithProviderDelivery(delivery.StreamingDelivery(delivery.FramingSSE))
	if contract.ClientDelivery != delivery.BufferedDelivery() {
		t.Fatalf("ingress delivery mode mismatch")
	}
	if contract.ProviderDelivery != delivery.StreamingDelivery(delivery.FramingSSE) {
		t.Fatalf("provider delivery mode mismatch")
	}
}

func TestExecutionContractValidate(t *testing.T) {
	valid := NewExecutionContractForDeliveries(delivery.BufferedDelivery(), delivery.StreamingDelivery(delivery.FramingSSE))
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid contract validate error: %v", err)
	}

	invalidDelivery := NewExecutionContractForDeliveries(delivery.Delivery{Mode: delivery.Buffered, Framing: delivery.FramingSSE}, delivery.BufferedDelivery())
	if err := invalidDelivery.Validate(); err == nil {
		t.Fatalf("expected invalid ingress delivery contract error")
	}

	badConversion := valid
	badConversion.ConversionKind = 0
	if err := badConversion.Validate(); err == nil {
		t.Fatalf("expected inconsistent conversion kind error")
	}
}

func TestEnvelopeReader_Close(t *testing.T) {
	resp := canonical.NewSliceEventReader(canonical.EventSequence{{ExchangeID: "ex", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "id", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}}})
	if resp == nil {
		t.Fatal("envelope stream = nil")
	}
	if err := resp.Close(context.Background()); err != nil {
		t.Fatalf("close error: %v", err)
	}
}

func TestProviderIngressValidate_RequiresExactlyOneCarrier(t *testing.T) {
	var none ProviderIngress
	if err := ValidateProviderIngress(none); err == nil {
		t.Fatal("expected error")
	}
	if err := ValidateProviderIngress(carrier.NewCarrierDocument(
		carrier.StageProviderIngressIn,
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte("x"),
		carrier.Meta{},
	)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateProviderIngress(carrier.CarrierStream{
		Stage:   carrier.StageProviderIngressIn,
		Family:  protocolkind.Responses,
		Framing: carrier.FramingSSE,
		Frames:  carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(""))),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateProviderIngress(carrier.CanonicalEventStream{Events: canonical.NewSliceEventReader([]canonical.Event{{
		ExchangeID: "ex",
		Seq:        1,
		Kind:       canonical.EventEnvelopeStart,
		EnvID:      "resp_1",
		Payload: canonical.EnvelopeStartPayload{
			Kind: canonical.EnvResponse,
		},
	}})}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

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
			input: carrier.NewCarrierDocument(
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
			input: carrier.CarrierStream{
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
			err := ValidateProviderIngress(tc.input)
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

func TestNewProviderRequest_OptionalEffectSink(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: "m"})
	target := NewRoutableTarget("b", "spec", "http://x", "c", "chat_completions", "", "")
	wireRequest := carrier.NewCarrierDocument(carrier.StageProviderRequestOut, protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{})

	reqNone := NewProviderRequest("ex", protocolkind.Responses, request, wireRequest, NewExecutionContract(delivery.BufferedDelivery()), target)
	if reqNone.EffectSink != nil {
		t.Fatal("expected nil effect sink when not provided")
	}

	sink := effect.NoopSink{}
	reqWith := NewProviderRequest("ex", protocolkind.Responses, request, wireRequest, NewExecutionContract(delivery.BufferedDelivery()), target, sink)
	if reqWith.EffectSink != sink {
		t.Fatal("expected effect sink when provided")
	}
}
