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
	clientByFamily map[canonical.ClientFamily]exchange.ClientCodec
	providerByKind map[protocolkind.ProtocolKind]protocolBundle
}

func NewResolver() RuntimeResolver {
	return RuntimeResolver{
		clientByFamily: map[canonical.ClientFamily]exchange.ClientCodec{
			canonical.ClientFamilyChatCompletions: ClientCodecBundle{
				request:  chatcompletions.ClientRequestDecoder{},
				document: chatcompletions.ResponseDocumentEncoder{},
				stream:   chatcompletions.ResponseStreamEncoder{},
			},
			canonical.ClientFamilyResponses: ClientCodecBundle{
				request:  responses.ClientRequestDecoder{},
				document: responses.ResponseDocumentEncoder{},
				stream:   responses.ResponseStreamEncoder{},
			},
			canonical.ClientFamilyCompletions: ClientCodecBundle{
				request:  completions.ClientRequestDecoder{},
				document: completions.ResponseDocumentEncoder{},
				stream:   completions.ResponseStreamEncoder{},
			},
			canonical.ClientFamilyMessages: ClientCodecBundle{
				request:  messages.ClientRequestDecoder{},
				document: messages.ResponseDocumentEncoder{},
				stream:   messages.ResponseStreamEncoder{},
			},
		},
		providerByKind: map[protocolkind.ProtocolKind]protocolBundle{
			protocolkind.ChatCompletions: {
				requestEncoder:  chatcompletions.ProviderRequestDocumentEncoder{},
				streamDecoder:   chatcompletions.ProviderEnvelopeDecoder{},
				documentDecoder: chatcompletions.ProviderDocumentDecoder{},
			},
			protocolkind.Responses: {
				requestEncoder:  responses.ProviderRequestDocumentEncoder{},
				streamDecoder:   responses.ProviderEnvelopeDecoder{},
				documentDecoder: responses.ProviderDocumentDecoder{},
			},
			protocolkind.Completions: {
				requestEncoder:  completions.ProviderRequestDocumentEncoder{},
				streamDecoder:   completions.ProviderEnvelopeDecoder{},
				documentDecoder: completions.ProviderDocumentDecoder{},
			},
			protocolkind.Messages: {
				requestEncoder:  messages.ProviderRequestDocumentEncoder{},
				streamDecoder:   messages.ProviderEnvelopeDecoder{},
				documentDecoder: messages.ProviderDocumentDecoder{},
			},
		},
	}
}

func (r RuntimeResolver) ClientCodec(f canonical.ClientFamily) exchange.ClientCodec {
	return r.clientByFamily[f]
}

func (r RuntimeResolver) ProviderRequestDocumentEncoder(kind protocolkind.ProtocolKind) exchange.ProviderRequestDocumentEncoder {
	return r.providerByKind[kind].requestEncoder
}

func (r RuntimeResolver) ProviderEnvelopeDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) exchange.ProviderEnvelopeDecoder {
	if d.Mode != delivery.Streaming {
		return nil
	}
	return r.providerByKind[kind].streamDecoder
}

func (r RuntimeResolver) ProviderDocumentDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) exchange.ProviderDocumentDecoder {
	if d.Mode != delivery.Buffered {
		return nil
	}
	return r.providerByKind[kind].documentDecoder
}

type ClientCodecBundle struct {
	request interface {
		DecodeClientRequest(carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error)
	}
	document interface {
		EncodeResponseDocument(canonical.CanonicalOutput) (carrier.WireDocument, error)
	}
	stream interface {
		EncodeResponseStream(canonical.EventReader, delivery.Delivery) (carrier.WireStream, error)
	}
}

func (b ClientCodecBundle) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	return b.request.DecodeClientRequest(doc)
}

func (b ClientCodecBundle) EncodeResponseDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	return b.document.EncodeResponseDocument(output)
}

func (b ClientCodecBundle) EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error) {
	return b.stream.EncodeResponseStream(events, d)
}
