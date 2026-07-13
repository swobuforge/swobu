// Package codecresolver composes wire protocol family codecs into a
// RuntimeCodecResolver that satisfies the exchange.RuntimeResolver interface.
//
// It is deliberately placed in an exchange sub-package so that the wire
// packages (which should not import exchange) do not create an import cycle.
package codecresolver

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/wire"
	chatcompletions "github.com/swobuforge/swobu/internal/wire/chatcompletions"
	completions "github.com/swobuforge/swobu/internal/wire/completions"
	messages "github.com/swobuforge/swobu/internal/wire/messages"
	responses "github.com/swobuforge/swobu/internal/wire/responses"
)

// RuntimeCodecResolver composes one exchange runtime protocol bundle set for all
// supported client families and provider protocol kinds.
//
// It owns explicit protocol-family bundle composition for one running Swobu process.
// It does not own daemon lifecycle, endpoint resolution, or provider execution,
// and it does not act as a registry-style switchboard.
type RuntimeCodecResolver struct {
	chatCompletionsClient wire.ClientCodec
	responsesClient       wire.ClientCodec
	completionsClient     wire.ClientCodec
	messagesClient        wire.ClientCodec

	chatCompletionsProvider protocolBundle
	responsesProvider       protocolBundle
	completionsProvider     protocolBundle
	messagesProvider        protocolBundle
}

// NewRuntimeCodecResolver returns a fully wired codec resolver.
func NewRuntimeCodecResolver() RuntimeCodecResolver {
	return RuntimeCodecResolver{
		chatCompletionsClient: clientCodecBundle{
			request:  chatcompletions.ClientRequestDecoder{},
			document: chatcompletions.ResponseDocumentEncoder{},
			stream:   chatcompletions.ResponseStreamEncoder{},
		},
		responsesClient: clientCodecBundle{
			request:  responses.ClientRequestDecoder{},
			document: responses.ResponseDocumentEncoder{},
			stream:   responses.ResponseStreamEncoder{},
		},
		completionsClient: clientCodecBundle{
			request:  completions.ClientRequestDecoder{},
			document: completions.ResponseDocumentEncoder{},
			stream:   completions.ResponseStreamEncoder{},
		},
		messagesClient: clientCodecBundle{
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

// ClientCodec returns the client codec for the given family.
func (r RuntimeCodecResolver) ClientCodec(f canonical.ClientFamily) wire.ClientCodec {
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

// ProviderRequestDocumentEncoder returns the encoder for the given protocol kind.
func (r RuntimeCodecResolver) ProviderRequestDocumentEncoder(kind protocolkind.ProtocolKind) wire.ProviderRequestDocumentEncoder {
	return r.providerBundle(kind).requestEncoder
}

// ProviderEnvelopeDecoder returns the envelope decoder for streaming deliveries.
func (r RuntimeCodecResolver) ProviderEnvelopeDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) wire.ProviderEnvelopeDecoder {
	if d.Mode != delivery.Streaming {
		return nil
	}
	return r.providerBundle(kind).streamDecoder
}

// ProviderDocumentDecoder returns the document decoder for buffered deliveries.
func (r RuntimeCodecResolver) ProviderDocumentDecoder(kind protocolkind.ProtocolKind, d delivery.Delivery) wire.ProviderDocumentDecoder {
	if d.Mode != delivery.Buffered {
		return nil
	}
	return r.providerBundle(kind).documentDecoder
}

func (r RuntimeCodecResolver) providerBundle(kind protocolkind.ProtocolKind) protocolBundle {
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

type protocolBundle struct {
	requestEncoder  wire.ProviderRequestDocumentEncoder
	streamDecoder   wire.ProviderEnvelopeDecoder
	documentDecoder wire.ProviderDocumentDecoder
}

// clientCodecBundle bridges three separate decoder/encoder types into one
// wire.ClientCodec. It is a composition convenience, not a semantic type.
type clientCodecBundle struct {
	request interface {
		DecodeClientRequest(carrier.WireDocument) (effect.Result[wire.ClientRequestResult], error)
	}
	document interface {
		EncodeResponseDocument(canonical.CanonicalOutput) (effect.Result[carrier.WireDocument], error)
	}
	stream interface {
		EncodeResponseStream(canonical.EventReader, delivery.Delivery) (effect.Result[carrier.WireStream], error)
	}
}

func (b clientCodecBundle) DecodeClientRequest(doc carrier.WireDocument) (effect.Result[wire.ClientRequestResult], error) {
	return b.request.DecodeClientRequest(doc)
}

func (b clientCodecBundle) EncodeResponseDocument(output canonical.CanonicalOutput) (effect.Result[carrier.WireDocument], error) {
	return b.document.EncodeResponseDocument(output)
}

func (b clientCodecBundle) EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (effect.Result[carrier.WireStream], error) {
	return b.stream.EncodeResponseStream(events, d)
}
