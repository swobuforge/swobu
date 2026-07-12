package chatcompletions

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
)

type legacyClientRequestDecoder struct{}

func (legacyClientRequestDecoder) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	result, err := (ClientRequestDecoder{}).DecodeClientRequest(doc)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	return result.Value.Request, result.Value.Delivery, nil
}

func (legacyClientRequestDecoder) DecodeClientRequestWithEffects(doc carrier.WireDocument, sink effect.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	return (ClientRequestDecoder{}).decodeClientRequestWithEffects(doc, sink, exchangeID)
}

type legacyResponseDocumentEncoder struct{}

func (legacyResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	result, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(output)
	return result.Value, err
}

type legacyResponseStreamEncoder struct{}

func (legacyResponseStreamEncoder) EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error) {
	result, err := (ResponseStreamEncoder{}).EncodeResponseStream(events, d)
	return result.Value, err
}

type legacyProviderDocumentDecoder struct{}

func (legacyProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, doc carrier.WireDocument, exchangeID string, _ effect.Sink) (canonical.EventReader, error) {
	result, err := (ProviderDocumentDecoder{}).DecodeProviderDocument(ctx, doc, exchangeID)
	return result.Value, err
}

type legacyProviderEnvelopeDecoder struct{}

func (legacyProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.WireStream, exchangeID string, _ effect.Sink) canonical.EventReader {
	result, _ := (ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(stream, exchangeID)
	return result.Value
}
