package ports

import (
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
)

func TestNewProviderRequest_ClonesCanonicalRequestAndTargetInputs(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: "m", Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")}})
	target := exchange.NewRoutableTarget("backend-a", "openai_"+"com"+"patible", "http://localhost:8080/v1", "cred-1", "chat_completions", "", "", "")
	req := NewProviderRequest(request, NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)), target)
	if req.Contract.ClientDelivery.Mode != delivery.Streaming || req.Contract.ProviderDelivery.Mode != delivery.Streaming {
		t.Fatalf("delivery clone mismatch")
	}
}

func TestExecutionContract_WithProviderDelivery_OverridesProviderDeliveryOnly(t *testing.T) {
	contract := NewExecutionContract(delivery.BufferedDelivery()).WithProviderDelivery(delivery.StreamingDelivery(delivery.FramingSSE))
	if contract.ClientDelivery.Mode != delivery.Buffered {
		t.Fatalf("client delivery mode mismatch")
	}
	if contract.ProviderDelivery.Mode != delivery.Streaming {
		t.Fatalf("provider delivery mode mismatch")
	}
}

func TestNewEnvelopeStreamingProviderResponseStream_Envelope(t *testing.T) {
	resp := NewEnvelopeStreamingProviderResponseStream(canonical.NewSliceEventReader(canonical.EventSequence{{ExchangeID: "ex", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "id", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}}}))
	if resp.EnvelopeStream() == nil {
		t.Fatal("envelope stream = nil")
	}
	if err := CloseProviderResponseStream(resp); err != nil {
		t.Fatalf("close error: %v", err)
	}
}

func TestProviderTransportResponseValidate_RequiresExactlyOneCarrier(t *testing.T) {
	if err := (ProviderTransportResponse{}).Validate(); err == nil {
		t.Fatal("expected error")
	}
	if err := (ProviderTransportResponse{Document: []byte("x"), Stream: io.NopCloser(strings.NewReader(""))}).Validate(); err == nil {
		t.Fatal("expected error")
	}
	if err := (ProviderTransportResponse{Document: []byte("x")}).Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := (ProviderTransportResponse{Stream: io.NopCloser(strings.NewReader(""))}).Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
