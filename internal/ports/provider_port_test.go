package ports

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/exchange"
)

func TestNewProviderRequest_ClonesCanonicalRequestAndTargetInputs(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: "m", Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")}})
	target := exchange.NewRoutableTarget("backend-a", "openai_"+"com"+"patible", "http://localhost:8080/v1", "cred-1", "chat_completions", "", "", "")
	req := NewProviderRequest(request, NewExecutionContract(protocolsurface.StreamingDelivery(protocolsurface.FramingSSE)), target)
	if req.Contract.ClientDelivery.Variant != protocolsurface.DeliveryVariantStreaming || req.Contract.ProviderDelivery.Variant != protocolsurface.DeliveryVariantStreaming {
		t.Fatalf("delivery clone mismatch")
	}
}

func TestExecutionContract_WithProviderDelivery_OverridesProviderDeliveryOnly(t *testing.T) {
	contract := NewExecutionContract(protocolsurface.BufferedDelivery()).WithProviderDelivery(protocolsurface.StreamingDelivery(protocolsurface.FramingSSE))
	if contract.ClientDelivery.Variant != protocolsurface.DeliveryVariantBuffered {
		t.Fatalf("ingress delivery mode mismatch")
	}
	if contract.ProviderDelivery.Variant != protocolsurface.DeliveryVariantStreaming {
		t.Fatalf("provider delivery mode mismatch")
	}
}

func TestExecutionContractValidate(t *testing.T) {
	valid := NewExecutionContractForDeliveries(protocolsurface.BufferedDelivery(), protocolsurface.StreamingDelivery(protocolsurface.FramingSSE))
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid contract validate error: %v", err)
	}

	invalidDelivery := NewExecutionContractForDeliveries(protocolsurface.Delivery{Variant: protocolsurface.DeliveryVariantBuffered, Framing: protocolsurface.FramingSSE}, protocolsurface.BufferedDelivery())
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
	if err := exchange.ValidateProviderIngress(none); err == nil {
		t.Fatal("expected error")
	}
	if err := exchange.ValidateProviderIngress(carrier.NewWireDocument(
		carrier.StageProviderIngressIn,
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte("x"),
		carrier.Meta{},
	)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := exchange.ValidateProviderIngress(carrier.WireStream{
		Stage:   carrier.StageProviderIngressIn,
		Family:  protocolkind.Responses,
		Framing: carrier.FramingSSE,
		Frames:  carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(""))),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := exchange.ValidateProviderIngress(carrier.CanonicalEventStream{Events: canonical.NewSliceEventReader([]canonical.Event{{
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
