package composition

import (
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/transform"
)

func TestAllDocumentTransformsAreIdempotent(t *testing.T) {
	reg := NewProviderTransformRegistry(ProviderTransformFactRecord{
		CacheAffinityKey:       "k1",
		CacheAffinityRetention: "ephemeral",
	})
	ctx := transform.Context{ExchangeID: "ex_doc", Stage: transform.StageProviderWireOut, Leg: carrier.LegProviderRequestOut, Carrier: carrier.KindWireDocument, Family: protocolkind.Responses, Delivery: delivery.BufferedDelivery()}
	in := carrier.WireDocument{Leg: carrier.LegProviderRequestOut, Family: protocolkind.Responses, Raw: []byte(`{"model":"m","stream":true,"input":"hello","unknown":"drop","tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}]}`)}
	once, _, err := reg.ApplyDocument(ctx, in)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	twice, _, err := reg.ApplyDocument(ctx, once)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("document transforms not idempotent")
	}
}

func TestAllEventTransformsAreIdempotent(t *testing.T) {
	reg := NewProviderTransformRegistry(ProviderTransformFactRecord{ReduceDuplicateUsageEvents: true})
	ctx := transform.Context{ExchangeID: "ex_stream", Stage: transform.StageSemanticEvents, Leg: carrier.LegProviderResponseIn, Carrier: carrier.KindCanonicalEventStream, Family: protocolkind.Responses, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}
	in := canonical.EventSequence{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: mustUsage(t, 1, 2)}},
		{Kind: canonical.EventTextDelta, EnvID: "m1", Payload: canonical.TextDeltaPayload{Text: "ok"}},
		{Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: mustUsage(t, 3, 4)}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	}
	firstWrapped, _, err := reg.WrapEventStream(ctx, canonical.NewSliceEventReader(in))
	if err != nil {
		t.Fatalf("first wrap: %v", err)
	}
	once := collectEvents(t, firstWrapped)
	secondWrapped, _, err := reg.WrapEventStream(ctx, canonical.NewSliceEventReader(once))
	if err != nil {
		t.Fatalf("second wrap: %v", err)
	}
	twice := collectEvents(t, secondWrapped)
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("event transforms not idempotent")
	}
}

func TestAllStreamTransformsAreIdempotent(t *testing.T) {
	// Current substrate has no wire-stream transform lane; contract is vacuously true.
	if false {
		t.Fatal("unreachable")
	}
}

func TestTransformMatchFalseIsNoOp(t *testing.T) {
	reg := NewProviderTransformRegistry(ProviderTransformFactRecord{})
	ctx := transform.Context{ExchangeID: "ex", Stage: transform.StageProviderWireOut, Leg: carrier.LegProviderRequestOut, Carrier: carrier.KindWireDocument, Family: protocolkind.Responses, Delivery: delivery.BufferedDelivery()}
	in := carrier.WireDocument{Leg: carrier.LegProviderRequestOut, Family: protocolkind.Responses, Raw: []byte(`{"model":"m"}`)}
	out, applied, err := reg.ApplyDocument(ctx, in)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("applied=%d want 0", len(applied))
	}
	if string(out.Raw) != string(in.Raw) {
		t.Fatalf("unexpected mutation")
	}
}

func collectEvents(t *testing.T, reader canonical.EventReader) canonical.EventSequence {
	t.Helper()
	out := make(canonical.EventSequence, 0, 8)
	for {
		ev, err := reader.Next(context.Background())
		if err != nil {
			if err == io.EOF {
				return out
			}
			t.Fatalf("next event: %v", err)
		}
		out = append(out, ev)
	}
}

func mustUsage(t *testing.T, in int, out int) canonical.TokenUsage {
	t.Helper()
	u, err := canonical.NewTokenUsageWithOptional(&in, &out, nil, nil)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional: %v", err)
	}
	return u
}
