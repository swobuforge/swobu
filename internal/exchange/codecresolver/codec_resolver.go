// Package codecresolver composes wire protocol family codecs into a
// RuntimeCodecResolver that satisfies the exchange.RuntimeResolver interface.
//
// It is deliberately placed in an exchange sub-package so that the wire
// packages (which should not import exchange) do not create an import cycle.
package codecresolver

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	chatcompletions "github.com/swobuforge/swobu/internal/wire/chatcompletions"
	messages "github.com/swobuforge/swobu/internal/wire/messages"
	responses "github.com/swobuforge/swobu/internal/wire/responses"
)

// RuntimeCodecResolver composes client-facing codecs for all supported client
// families. Exact provider codec composition lives behind provider backends.
//
// It owns explicit protocol-family bundle composition for one running Swobu process.
// It does not own daemon lifecycle, endpoint resolution, or provider execution,
// and it does not act as a registry-style switchboard.
type RuntimeCodecResolver struct {
	chatCompletionsClient wire.ClientCodec
	responsesClient       wire.ClientCodec
	messagesClient        wire.ClientCodec
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
		messagesClient: clientCodecBundle{
			request:  messages.ClientRequestDecoder{},
			document: messages.ResponseDocumentEncoder{},
			stream:   messages.ResponseStreamEncoder{},
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
	case canonical.ClientFamilyMessages:
		return r.messagesClient
	default:
		return nil
	}
}

// clientCodecBundle bridges three separate decoder/encoder types into one
// wire.ClientCodec. It is a composition convenience, not a semantic type.
type clientCodecBundle struct {
	request interface {
		DecodeClientRequest(carrier.Document) (wire.ClientDecodeResult, error)
	}
	document interface {
		EncodeResponseDocument(canonical.CanonicalOutput) (wire.ClientDocumentResult, error)
	}
	stream interface {
		EncodeResponseStream(context.Context, canonical.ResponseStream, delivery.Delivery) (wire.ClientByteStreamResult, error)
		EncodeResponseMessages(context.Context, canonical.ResponseStream, delivery.Delivery) (wire.ClientMessageResult, error)
	}
}

func (b clientCodecBundle) DecodeClientRequest(doc carrier.Document) (wire.ClientDecodeResult, error) {
	return b.request.DecodeClientRequest(doc)
}

func (b clientCodecBundle) EncodeResponseDocument(output canonical.CanonicalOutput) (wire.ClientDocumentResult, error) {
	return b.document.EncodeResponseDocument(output)
}

func (b clientCodecBundle) EncodeResponseStream(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientByteStreamResult, error) {
	return b.stream.EncodeResponseStream(ctx, events, d)
}

func (b clientCodecBundle) EncodeResponseMessages(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientMessageResult, error) {
	return b.stream.EncodeResponseMessages(ctx, events, d)
}
