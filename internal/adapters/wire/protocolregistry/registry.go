package protocolregistry

import (
	chatcompletions "github.com/swobuforge/swobu/internal/adapters/wire/families/chatcompletions"
	completions "github.com/swobuforge/swobu/internal/adapters/wire/families/completions"
	messages "github.com/swobuforge/swobu/internal/adapters/wire/families/messages"
	responses "github.com/swobuforge/swobu/internal/adapters/wire/families/responses"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type ClientRequestDecoder interface {
	DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error)
}

type ClientDocumentEncoder interface {
	EncodeClientDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error)
}

type ClientStreamEncoder interface {
	EncodeClientStream(events canonical.EventReader) (carrier.WireStream, error)
}

type ProviderStreamDecoder interface {
	DecodeProviderStream(stream carrier.WireStream, exchangeID string) canonical.EventReader
}

type ResponsesEncodeOptions = responses.EncodeOptions

type ResponsesEncodeOptionsCarrierCodec interface {
	EncodeProviderRequestWithOptions(request canonical.CanonicalRequest, delivery delivery.Delivery, options ResponsesEncodeOptions) (carrier.WireDocument, error)
}

type ProviderRequestEncoder interface {
	EncodeProviderRequest(request canonical.CanonicalRequest, delivery delivery.Delivery) (carrier.WireDocument, error)
}

type ProviderDocumentDecoder interface {
	DecodeProviderDocument(doc carrier.WireDocument, exchangeID string) (canonical.EventReader, error)
}

func ForClientRequestDecoder(family canonical.IngressFamily) (ClientRequestDecoder, error) {
	switch family {
	case canonical.IngressFamilyChatCompletions:
		return chatcompletions.ClientRequestDecoder{}, nil
	case canonical.IngressFamilyResponses:
		return responses.ClientRequestDecoder{}, nil
	case canonical.IngressFamilyCompletions:
		return completions.ClientRequestDecoder{}, nil
	case canonical.IngressFamilyMessages:
		return messages.ClientRequestDecoder{}, nil
	default:
		return nil, canonical.UnsupportedOperation("ingress family is not implemented")
	}
}

func ForClientDocumentEncoder(family canonical.IngressFamily) (ClientDocumentEncoder, error) {
	switch family {
	case canonical.IngressFamilyChatCompletions:
		return chatcompletions.ClientDocumentEncoder{}, nil
	case canonical.IngressFamilyResponses:
		return responses.ClientDocumentEncoder{}, nil
	case canonical.IngressFamilyCompletions:
		return completions.ClientDocumentEncoder{}, nil
	case canonical.IngressFamilyMessages:
		return messages.ClientDocumentEncoder{}, nil
	default:
		return nil, canonical.UnsupportedOperation("ingress family is not implemented")
	}
}

func ForClientStreamEncoder(family canonical.IngressFamily) (ClientStreamEncoder, error) {
	switch family {
	case canonical.IngressFamilyChatCompletions:
		return chatcompletions.ClientStreamEncoder{}, nil
	case canonical.IngressFamilyResponses:
		return responses.ClientStreamEncoder{}, nil
	case canonical.IngressFamilyCompletions:
		return completions.ClientStreamEncoder{}, nil
	case canonical.IngressFamilyMessages:
		return messages.ClientStreamEncoder{}, nil
	default:
		return nil, canonical.UnsupportedOperation("ingress family is not implemented")
	}
}

func ForProviderResponseDocumentProtocolCarrierEnvelope(kind protocolkind.ProtocolKind) (ProviderDocumentDecoder, error) {
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ProviderDocumentDecoder{}, nil
	case protocolkind.Responses:
		return responses.ProviderDocumentDecoder{}, nil
	case protocolkind.Completions:
		return completions.ProviderDocumentDecoder{}, nil
	case protocolkind.Messages:
		return messages.ProviderDocumentDecoder{}, nil
	default:
		return nil, canonical.UnsupportedOperation("protocol kind is not implemented")
	}
}

func forProviderResponseStreamProtocol(kind protocolkind.ProtocolKind) (ProviderStreamDecoder, error) {
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ProviderStreamDecoder{}, nil
	case protocolkind.Responses:
		return responses.ProviderStreamDecoder{}, nil
	case protocolkind.Completions:
		return completions.ProviderStreamDecoder{}, nil
	case protocolkind.Messages:
		return messages.ProviderStreamDecoder{}, nil
	default:
		return nil, canonical.UnsupportedOperation("protocol kind is not implemented")
	}
}

func ForProviderRequestProtocolCarrier(kind protocolkind.ProtocolKind) (ProviderRequestEncoder, error) {
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ProviderRequestEncoder{}, nil
	case protocolkind.Responses:
		return responses.ProviderRequestEncoder{}, nil
	case protocolkind.Completions:
		return completions.ProviderRequestEncoder{}, nil
	case protocolkind.Messages:
		return messages.ProviderRequestEncoder{}, nil
	default:
		return nil, canonical.UnsupportedOperation("protocol kind is not implemented")
	}
}

type providerStreamDecoderAdapter struct{ codec ProviderStreamDecoder }

func (a providerStreamDecoderAdapter) DecodeProviderStream(stream carrier.WireStream, exchangeID string) canonical.EventReader {
	return a.codec.DecodeProviderStream(stream, exchangeID)
}

func ForProviderResponseStreamProtocolCarrier(kind protocolkind.ProtocolKind) (ProviderStreamDecoder, error) {
	codec, err := forProviderResponseStreamProtocol(kind)
	if err != nil {
		return nil, err
	}
	return providerStreamDecoderAdapter{codec: codec}, nil
}
