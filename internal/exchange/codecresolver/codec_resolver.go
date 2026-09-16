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
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
	chatcompletions "github.com/swobuforge/swobu/internal/wire/chatcompletions"
	generatecontent "github.com/swobuforge/swobu/internal/wire/generatecontent"
	messages "github.com/swobuforge/swobu/internal/wire/messages"
	responses "github.com/swobuforge/swobu/internal/wire/responses"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
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
	generateContentClient wire.ClientCodec
}

// NewRuntimeCodecResolver returns a fully wired codec resolver.
func NewRuntimeCodecResolver() RuntimeCodecResolver {
	resources := provider.DefaultMediaLimits()
	imageLimits := shared.ImageDecodeLimitPolicy{MaxInlineBytes: int(resources.MaxImageBytes), MaxImages: resources.MaxImages, MaxTotalImageBytes: int(resources.MaxTotalImageBytes)}
	return RuntimeCodecResolver{
		chatCompletionsClient: clientCodecBundle{
			decode: func(doc carrier.Document, _ canonical.ClientOperation) (wire.ClientDecodeResult, error) {
				return (chatcompletions.ClientRequestDecoder{ImageLimits: imageLimits}).DecodeClientRequest(doc)
			},
			document: chatcompletions.ResponseDocumentEncoder{},
			stream:   chatcompletions.ResponseStreamEncoder{},
		},
		responsesClient: clientCodecBundle{
			decode: func(doc carrier.Document, _ canonical.ClientOperation) (wire.ClientDecodeResult, error) {
				return (responses.ClientRequestDecoder{ImageLimits: imageLimits}).DecodeClientRequest(doc)
			},
			document: responses.ResponseDocumentEncoder{},
			stream:   responses.ResponseStreamEncoder{},
		},
		messagesClient: clientCodecBundle{
			decode: func(doc carrier.Document, _ canonical.ClientOperation) (wire.ClientDecodeResult, error) {
				return (messages.ClientRequestDecoder{ImageLimits: imageLimits}).DecodeClientRequest(doc)
			},
			document: messages.ResponseDocumentEncoder{},
			stream:   messages.ResponseStreamEncoder{},
		},
		generateContentClient: clientCodecBundle{
			decode: func(doc carrier.Document, operation canonical.ClientOperation) (wire.ClientDecodeResult, error) {
				return (generatecontent.ClientRequestDecoder{ImageLimits: imageLimits}).DecodeClientRequest(doc, operation)
			}, document: generatecontent.ResponseDocumentEncoder{}, stream: generatecontent.ResponseStreamEncoder{},
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
	case canonical.ClientFamilyGenerateContent:
		return r.generateContentClient
	default:
		return nil
	}
}

// clientCodecBundle bridges three separate decoder/encoder types into one
// wire.ClientCodec. It is a composition convenience, not a semantic type.
type clientCodecBundle struct {
	decode   func(carrier.Document, canonical.ClientOperation) (wire.ClientDecodeResult, error)
	document interface {
		EncodeResponseDocument(canonical.CanonicalRequest, canonical.CanonicalResponse) (wire.ClientDocumentResult, error)
	}
	stream interface {
		EncodeResponseStream(context.Context, canonical.CanonicalRequest, canonical.ResponseStream, delivery.Delivery) (wire.ClientByteStreamResult, error)
		EncodeResponseMessages(context.Context, canonical.CanonicalRequest, canonical.ResponseStream, delivery.Delivery) (wire.ClientMessageResult, error)
	}
}

func (b clientCodecBundle) DecodeClientRequest(doc carrier.Document, operation canonical.ClientOperation) (wire.ClientDecodeResult, error) {
	return b.decode(doc, operation)
}

func (b clientCodecBundle) EncodeResponseDocument(request canonical.CanonicalRequest, output canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	return b.document.EncodeResponseDocument(request, output)
}

func (b clientCodecBundle) EncodeResponseStream(ctx context.Context, request canonical.CanonicalRequest, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientByteStreamResult, error) {
	return b.stream.EncodeResponseStream(ctx, request, events, d)
}

func (b clientCodecBundle) EncodeResponseMessages(ctx context.Context, request canonical.CanonicalRequest, events canonical.ResponseStream, d delivery.Delivery) (wire.ClientMessageResult, error) {
	return b.stream.EncodeResponseMessages(ctx, request, events, d)
}
