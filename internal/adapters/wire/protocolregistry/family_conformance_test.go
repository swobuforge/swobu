package protocolregistry

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestClientFamiliesImplementDirectionalClientPorts(t *testing.T) {
	t.Parallel()
	cases := []canonical.IngressFamily{
		canonical.IngressFamilyChatCompletions,
		canonical.IngressFamilyResponses,
		canonical.IngressFamilyMessages,
		canonical.IngressFamilyCompletions,
	}
	for _, family := range cases {
		family := family
		t.Run(string(family), func(t *testing.T) {
			t.Parallel()
			codec, err := ForClientFamily(family)
			if err != nil {
				t.Fatalf("ForClientFamily(%s): %v", family, err)
			}
			if _, ok := codec.(ClientRequestDecoder); !ok {
				t.Fatalf("family %s missing ClientRequestDecoder", family)
			}
			if _, ok := codec.(ClientDocumentEncoder); !ok {
				t.Fatalf("family %s missing ClientDocumentEncoder", family)
			}
			if _, ok := codec.(ClientStreamEncoder); !ok {
				t.Fatalf("family %s missing ClientStreamEncoder", family)
			}
		})
	}
}

func TestProviderResponseCodecsImplementDirectionalProviderPorts(t *testing.T) {
	t.Parallel()
	cases := []protocolkind.ProtocolKind{
		protocolkind.ChatCompletions,
		protocolkind.Responses,
		protocolkind.Messages,
		protocolkind.Completions,
	}
	for _, kind := range cases {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			docDecoder, err := ForProviderResponseDocumentProtocolCarrierEnvelope(kind)
			if err != nil {
				t.Fatalf("ForProviderResponseDocumentProtocolCarrierEnvelope(%s): %v", kind, err)
			}
			if _, ok := docDecoder.(ProviderDocumentDecoder); !ok {
				t.Fatalf("kind %s missing ProviderDocumentDecoder", kind)
			}
			streamDecoder, err := ForProviderResponseStreamProtocolCarrier(kind)
			if err != nil {
				t.Fatalf("ForProviderResponseStreamProtocolCarrier(%s): %v", kind, err)
			}
			if _, ok := streamDecoder.(ProviderStreamDecoder); !ok {
				t.Fatalf("kind %s missing ProviderStreamDecoder", kind)
			}
		})
	}
}

func TestProviderRequestEncodersImplementDirectionalPort(t *testing.T) {
	t.Parallel()
	cases := []protocolkind.ProtocolKind{
		protocolkind.ChatCompletions,
		protocolkind.Responses,
		protocolkind.Messages,
		protocolkind.Completions,
	}
	for _, kind := range cases {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			encoder, err := ForProviderRequestProtocolCarrier(kind)
			if err != nil {
				t.Fatalf("ForProviderRequestProtocolCarrier(%s): %v", kind, err)
			}
			if _, ok := encoder.(ProviderRequestEncoder); !ok {
				t.Fatalf("kind %s missing ProviderRequestEncoder", kind)
			}
		})
	}
}
