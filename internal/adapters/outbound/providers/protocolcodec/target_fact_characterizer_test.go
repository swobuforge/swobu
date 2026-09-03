package protocolcodec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestCharacterizeTargetFactUsesValidIsolatedFixturesForEveryFact(t *testing.T) {
	tests := []struct {
		name     string
		fact     provider.TargetFact
		protocol protocolkind.Kind
		codec    Codec
	}{
		{name: "parallel tool calls false", fact: provider.AcceptsParallelToolCallsFalse, protocol: protocolkind.ChatCompletions},
		{name: "max completion tokens", fact: provider.AcceptsMaxCompletionTokens, protocol: protocolkind.ChatCompletions, codec: Codec{ChatDialect: ChatDialect{UseMaxCompletionTokens: true}}},
		{name: "reasoning effort max", fact: provider.AcceptsReasoningEffortMax, protocol: protocolkind.Responses},
		{name: "reasoning disabled", fact: provider.AcceptsReasoningDisabled, protocol: protocolkind.Responses},
		{name: "function call output array", fact: provider.AcceptsFunctionCallOutputArray, protocol: protocolkind.Responses},
		{name: "chat stream include usage", fact: provider.AcceptsChatStreamIncludeUsage, protocol: protocolkind.ChatCompletions},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			codec := test.codec
			codec.Protocol = test.protocol
			target := provider.TargetSnapshot{
				ProviderSpec: "custom", TargetID: "target", TargetVersion: 1,
				Model: "model", ProtocolKind: test.protocol,
			}
			request, ok := targetFactFixture(target.Model, test.fact)
			if !ok {
				t.Fatal("fixture construction failed")
			}
			if err := canonical.ValidateMaterializedRequest(request); err != nil {
				t.Fatalf("fixture is invalid canonical: %v", err)
			}
			preferred := encodeTargetFactFixture(t, codec, request, targetFactFixtureDelivery(test.fact), test.fact, true)
			control := encodeTargetFactFixture(t, codec, request, targetFactFixtureDelivery(test.fact), test.fact, false)
			if bytes.Equal(preferred, control) {
				t.Fatal("preferred and control fixtures do not differ")
			}

			t.Run("preferred completion resolves true", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					return completedTargetFactIngress(test.protocol, targetFactFixtureDelivery(test.fact)), nil
				}))
				if !resolution.Conclusive || !resolution.Value || calls != 1 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})

			t.Run("preferred rejection and control completion resolves false", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					if calls == 1 {
						return nil, provider.AttemptMayHaveExecuted(provider.Rejected(canonical.NewBackendError("target", 400, "rejected", "")))
					}
					return completedTargetFactIngress(test.protocol, targetFactFixtureDelivery(test.fact)), nil
				}))
				if !resolution.Conclusive || resolution.Value || calls != 2 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})

			t.Run("other preferred result is inconclusive", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					return nil, errors.New("transport unavailable")
				}))
				if resolution.Conclusive || calls != 1 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})

			t.Run("other control result is inconclusive", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					return nil, provider.AttemptMayHaveExecuted(provider.Rejected(canonical.NewBackendError("target", 400, "rejected", "")))
				}))
				if resolution.Conclusive || calls != 2 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})
		})
	}
}

func encodeTargetFactFixture(t *testing.T, codec Codec, request canonical.CanonicalRequest, fixtureDelivery delivery.Delivery, fact provider.TargetFact, value bool) []byte {
	t.Helper()
	facts := provider.NewTargetFacts(func(read provider.TargetFact) (bool, bool) {
		if read != fact {
			t.Fatalf("fixture read fact %v, want %v", read, fact)
		}
		return value, true
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := codec.Encode(provider.Request{
		Canonical: request, TargetFacts: facts, ToolNames: names, Delivery: fixtureDelivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	reads := facts.Reads()
	if len(reads) != 1 || reads[fact] != value {
		t.Fatalf("fixture reads = %#v, want only %v=%t", reads, fact, value)
	}
	return document.RawBytes()
}

func completedTargetFactIngress(protocol protocolkind.Kind, fixtureDelivery delivery.Delivery) provider.Ingress {
	if fixtureDelivery.IsStreaming() {
		raw := "data: {\"id\":\"response\",\"model\":\"model\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
			"data: {\"id\":\"response\",\"model\":\"model\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
			"data: {\"id\":\"response\",\"model\":\"model\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"
		return provider.StreamIngress{Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}}
	}
	raw := []byte(`{"id":"response","model":"model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	if protocol == protocolkind.Responses {
		raw = []byte(`{"id":"response","model":"model","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}]}`)
	}
	return provider.DocumentIngress{Document: carrier.NewDocument(protocol, "application/json", nil, raw, carrier.Meta{})}
}
