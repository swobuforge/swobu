package exchangeruntime

import (
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestResolver_ComposesAllClientFamilies(t *testing.T) {
	resolver := NewResolver()
	for _, family := range []canonical.ClientFamily{
		canonical.ClientFamilyChatCompletions,
		canonical.ClientFamilyResponses,
		canonical.ClientFamilyCompletions,
		canonical.ClientFamilyMessages,
	} {
		if resolver.ClientCodec(family) == nil {
			t.Fatalf("client codec missing for family %s", family)
		}
	}
}

func TestResolver_ComposesAllProviderProtocols(t *testing.T) {
	resolver := NewResolver()
	for _, kind := range []protocolkind.ProtocolKind{
		protocolkind.ChatCompletions,
		protocolkind.Responses,
		protocolkind.Completions,
		protocolkind.Messages,
	} {
		if resolver.ProviderRequestDocumentEncoder(kind) == nil {
			t.Fatalf("provider request encoder missing for protocol %s", kind)
		}
		if resolver.ProviderEnvelopeDecoder(kind, delivery.StreamingDelivery(delivery.FramingSSE)) == nil {
			t.Fatalf("provider stream decoder missing for protocol %s", kind)
		}
		if resolver.ProviderDocumentDecoder(kind, delivery.BufferedDelivery()) == nil {
			t.Fatalf("provider document decoder missing for protocol %s", kind)
		}
	}
}
