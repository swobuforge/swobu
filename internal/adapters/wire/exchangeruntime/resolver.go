package exchangeruntime

import (
	chatcompletions "github.com/swobuforge/swobu/internal/adapters/wire/families/chatcompletions"
	completions "github.com/swobuforge/swobu/internal/adapters/wire/families/completions"
	messages "github.com/swobuforge/swobu/internal/adapters/wire/families/messages"
	responses "github.com/swobuforge/swobu/internal/adapters/wire/families/responses"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

type protocolBundle struct {
	requestEncoder  exchange.ProviderRequestDocumentEncoder
	streamDecoder   exchange.ProviderEnvelopeDecoder
	documentDecoder exchange.ProviderDocumentDecoder
}

// RuntimeResolver composes one exchange runtime protocol bundle set for all supported
// client families and provider protocol kinds.
type RuntimeResolver struct {
	chatCompletionsClient exchange.ClientCodec
	responsesClient       exchange.ClientCodec
	completionsClient     exchange.ClientCodec
	messagesClient        exchange.ClientCodec

	chatCompletionsProvider protocolBundle
	responsesProvider       protocolBundle
	completionsProvider     protocolBundle
	messagesProvider        protocolBundle
}

func NewResolver() RuntimeResolver {
	return RuntimeResolver{
		chatCompletionsClient: ClientCodecBundle{
			request:  chatcompletions.ClientRequestDecoder{},
			document: chatcompletions.ResponseDocumentEncoder{},
			stream:   chatcompletions.ResponseStreamEncoder{},
		},
		responsesClient: ClientCodecBundle{
			request:  responses.ClientRequestDecoder{},
			document: responses.ResponseDocumentEncoder{},
			stream:   responses.ResponseStreamEncoder{},
		},
		completionsClient: ClientCodecBundle{
			request:  completions.ClientRequestDecoder{},
			document: completions.ResponseDocumentEncoder{},
			stream:   completions.ResponseStreamEncoder{},
		},
		messagesClient: ClientCodecBundle{
			request:  messages.ClientRequestDecoder{},
			document: messages.ResponseDocumentEncoder{},
			stream:   messages.ResponseStreamEncoder{},
		},
		chatCompletionsProvider: protocolBundle{
			requestEncoder:  chatcompletions.ProviderRequestDocumentEncoder{},
			streamDecoder:   chatcompletions.ProviderEnvelopeDecoder{},
			documentDecoder: chatcompletions.ProviderDocumentDecoder{},
		},
		responsesProvider: protocolBundle{
			requestEncoder:  responses.ProviderRequestDocumentEncoder{},
			streamDecoder:   responses.ProviderEnvelopeDecoder{},
			documentDecoder: responses.ProviderDocumentDecoder{},
		},
		completionsProvider: protocolBundle{
			requestEncoder:  completions.ProviderRequestDocumentEncoder{},
			streamDecoder:   completions.ProviderEnvelopeDecoder{},
			documentDecoder: completions.ProviderDocumentDecoder{},
		},
		messagesProvider: protocolBundle{
			requestEncoder:  messages.ProviderRequestDocumentEncoder{},
			streamDecoder:   messages.ProviderEnvelopeDecoder{},
			documentDecoder: messages.ProviderDocumentDecoder{},
		},
	}
}

func (r RuntimeResolver) ClientCodec(f canonical.ClientFamily) exchange.ClientCodec {
	switch f {
	case canonical.ClientFamilyChatCompletions:
		return r.chatCompletionsClient
	case canonical.ClientFamilyResponses:
		return r.responsesClient
	case canonical.ClientFamilyCompletions:
		return r.completionsClient
	case canonical.ClientFamilyMessages:
		return r.messagesClient
	default:
		return nil
	}
}

func (r RuntimeResolver) ProviderRequestDocumentEncoder(kind protocolkind.ProtocolKind) exchange.ProviderRequestDocumentEncoder {
	return r.providerBundle(kind).requestEncoder
}

func (r RuntimeResolver) ProviderEnvelopeDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) exchange.ProviderEnvelopeDecoder {
	if d.Mode != delivery.Streaming {
		return nil
	}
	return r.providerBundle(kind).streamDecoder
}

func (r RuntimeResolver) ProviderDocumentDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) exchange.ProviderDocumentDecoder {
	if d.Mode != delivery.Buffered {
		return nil
	}
	return r.providerBundle(kind).documentDecoder
}

func (r RuntimeResolver) providerBundle(kind protocolkind.ProtocolKind) protocolBundle {
	switch kind {
	case protocolkind.ChatCompletions:
		return r.chatCompletionsProvider
	case protocolkind.Responses:
		return r.responsesProvider
	case protocolkind.Completions:
		return r.completionsProvider
	case protocolkind.Messages:
		return r.messagesProvider
	default:
		return protocolBundle{}
	}
}

type ClientCodecBundle struct {
	request interface {
		DecodeClientRequest(carrier.WireDocument) (exchange.Result[exchange.ClientRequestResult], error)
	}
	document interface {
		EncodeResponseDocument(canonical.CanonicalOutput) (exchange.Result[carrier.WireDocument], error)
	}
	stream interface {
		EncodeResponseStream(canonical.EventReader, delivery.Delivery) (exchange.Result[carrier.WireStream], error)
	}
}

func (b ClientCodecBundle) DecodeClientRequest(doc carrier.WireDocument) (exchange.Result[exchange.ClientRequestResult], error) {
	return b.request.DecodeClientRequest(doc)
}

func (b ClientCodecBundle) EncodeResponseDocument(output canonical.CanonicalOutput) (exchange.Result[carrier.WireDocument], error) {
	return b.document.EncodeResponseDocument(output)
}

func (b ClientCodecBundle) EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (exchange.Result[carrier.WireStream], error) {
	return b.stream.EncodeResponseStream(events, d)
}
