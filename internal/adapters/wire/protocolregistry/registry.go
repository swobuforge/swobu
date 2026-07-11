package protocolregistry

import (
	"context"
	"errors"
	"io"

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

type ClientFamily interface {
	ClientRequestDecoder
	ClientDocumentEncoder
	ClientStreamEncoder
}

type clientFamilyCodec struct {
	requestDecoder          ClientRequestDecoder
	documentResponseEncoder ClientDocumentEncoder
	streamResponseEncoder   ClientStreamEncoder
}

func (f clientFamilyCodec) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	return f.requestDecoder.DecodeClientRequest(doc)
}

func (f clientFamilyCodec) EncodeClientDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	return f.documentResponseEncoder.EncodeClientDocument(output)
}

func (f clientFamilyCodec) EncodeClientStream(events canonical.EventReader) (carrier.WireStream, error) {
	return f.streamResponseEncoder.EncodeClientStream(events)
}

func ForClientFamily(family canonical.IngressFamily) (ClientFamily, error) {
	requestDecoder, err := forClientRequestFamily(family)
	if err != nil {
		return nil, err
	}
	documentResponseEncoder, err := forClientResponseDocumentFamily(family)
	if err != nil {
		return nil, err
	}
	streamResponseEncoder, err := forClientResponseStreamFamily(family)
	if err != nil {
		return nil, err
	}
	return clientFamilyCodec{requestDecoder: requestDecoder, documentResponseEncoder: documentResponseEncoder, streamResponseEncoder: streamResponseEncoder}, nil
}

func forClientRequestFamily(family canonical.IngressFamily) (ClientRequestDecoder, error) {
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

func forClientResponseDocumentFamily(family canonical.IngressFamily) (ClientDocumentEncoder, error) {
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

func forClientResponseStreamFamily(family canonical.IngressFamily) (ClientStreamEncoder, error) {
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

func EncodeClientStream(events canonical.EventReader, family protocolkind.ProtocolKind, encode func(canonical.Event) ([][]byte, error)) (carrier.WireStream, error) {
	if encode == nil {
		return carrier.WireStream{}, errors.New("client stream encoder is required")
	}
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = events.Close(context.Background()) }()
		defer func() { _ = pw.Close() }()
		for {
			ev, err := events.Next(context.Background())
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				_ = pw.CloseWithError(err)
				return
			}
			frames, err := encode(ev)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			for _, frame := range frames {
				if _, err := pw.Write(frame); err != nil {
					_ = pw.CloseWithError(err)
					return
				}
			}
		}
	}()
	return carrier.WireStream{Family: family, Framing: carrier.FramingSSE, Body: pr}, nil
}
