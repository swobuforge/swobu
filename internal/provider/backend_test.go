package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestProviderContractsHaveNoResumptionSidecar(t *testing.T) {
	for name, typ := range map[string]reflect.Type{
		"Request":         reflect.TypeOf(Request{}),
		"Backend":         reflect.TypeOf(Backend{}),
		"DecodedResponse": reflect.TypeOf(DecodedResponse{}),
	} {
		for _, field := range []string{"Continuation", "CaptureContinuation", "NativeContinuation"} {
			if _, ok := typ.FieldByName(field); ok {
				t.Fatalf("%s still exposes %s", name, field)
			}
		}
	}
}

func TestProviderRequestKeepsExecutionFactsInsideAttemptContext(t *testing.T) {
	assertFields := func(name string, typ reflect.Type, want []string) {
		t.Helper()
		if typ.NumField() != len(want) {
			t.Fatalf("%s has %d fields, want %d", name, typ.NumField(), len(want))
		}
		for index, field := range want {
			if typ.Field(index).Name != field {
				t.Fatalf("%s field %d = %q, want %q", name, index, typ.Field(index).Name, field)
			}
		}
	}

	assertFields("Request", reflect.TypeOf(Request{}), []string{
		"Attempt", "Canonical", "TargetFacts", "PreviousHistory", "EncodeContext", "Delivery", "ToolNames",
	})
	assertFields("AttemptContext", reflect.TypeOf(AttemptContext{}), []string{
		"ExchangeID", "ThreadID", "CacheLocality", "HasNextRouteCandidate",
	})
	assertFields("EncodeContext", reflect.TypeOf(EncodeContext{}), []string{"Context", "ResolveImage"})
}

func TestRequestDeliveryIsProviderFacingWireIntent(t *testing.T) {
	req := Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m")}),
		Delivery:  delivery.StreamingDelivery(delivery.FramingSSE),
	}
	if req.Delivery != delivery.StreamingDelivery(delivery.FramingSSE) {
		t.Fatalf("delivery = %#v", req.Delivery)
	}
}

func TestBackendValidationOwnsOnlyTargetCodecAndTransport(t *testing.T) {
	target := TargetSnapshot{
		TargetID: "target", TargetVersion: 1, ProviderSpec: "openai", Model: "m",
		ProtocolKind: protocolkind.Responses, ProviderProtocol: "responses",
	}
	backend := Backend{
		Target: target,
		Codec:  testBackendContractCodec{},
		Transport: TransportFunc(func(context.Context, carrier.Document) (Ingress, error) {
			return nil, nil
		}),
	}
	if err := backend.Validate(); err != nil {
		t.Fatalf("complete backend rejected: %v", err)
	}
}

type testBackendContractCodec struct{}

func (testBackendContractCodec) Encode(Request) (carrier.Document, []compat.Change, error) {
	return carrier.Document{}, nil, nil
}

func (testBackendContractCodec) Decode(context.Context, Request, Ingress) (DecodedResponse, error) {
	return DecodedResponse{}, nil
}
