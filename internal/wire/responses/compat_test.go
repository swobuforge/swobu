package responses

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type legacyClientRequestDecoder struct{}

func (legacyClientRequestDecoder) DecodeClientRequest(doc carrier.Document) (canonical.CanonicalRequest, delivery.Delivery, error) {
	result, err := (ClientRequestDecoder{}).DecodeClientRequest(doc)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	return result.Request.Request, result.Request.Delivery, nil
}

func (legacyClientRequestDecoder) DecodeClientRequestWithDecisions(doc carrier.Document, sink compat.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	return (ClientRequestDecoder{}).decodeClientRequestWithDecisions(doc, sink, exchangeID)
}

type legacyResponseDocumentEncoder struct{}

func (legacyResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalResponse) (carrier.Document, error) {
	result, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, output)
	return result.Document, err
}

type legacyResponseStreamEncoder struct{}

func (legacyResponseStreamEncoder) EncodeResponseStream(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (carrier.ByteStream, error) {
	result, err := (ResponseStreamEncoder{}).EncodeResponseStream(ctx, canonical.CanonicalRequest{}, events, d)
	return result.Stream, err
}

type legacyProviderDocumentDecoder struct{}

func (legacyProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, doc carrier.Document, exchangeID string, _ compat.Sink) (canonical.ResponseStream, error) {
	result, err := (ProviderDocumentDecoder{}).DecodeProviderDocument(ctx, canonical.CanonicalRequest{}, doc, exchangeID)
	return result.Stream, err
}

type legacyProviderEnvelopeDecoder struct{}

func (legacyProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.ByteStream, exchangeID string, _ compat.Sink) canonical.ResponseStream {
	result, _ := (ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(canonical.CanonicalRequest{}, stream, exchangeID)
	return result.Stream
}
