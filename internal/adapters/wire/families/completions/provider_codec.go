package completions

import (
	"context"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/adapters/wire/shared"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/exchange"
)

func (ProviderRequestDocumentEncoder) EncodeProviderRequestDocument(request canonical.CanonicalRequest, delivery delivery.Delivery, exchangeID string) (exchange.Result[carrier.WireDocument], error) {
	doc, err := EncodeCarrier(request, delivery)
	return exchange.NewResult(doc), err
}

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, doc carrier.WireDocument, exchangeID string) (exchange.Result[canonical.EventReader], error) {
	if err := core.ValidateResponseCarrierDocument(doc, protocolkind.Completions); err != nil {
		carrierErr := canonical.InternalError("completions response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return exchange.Result[canonical.EventReader]{Value: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (canonical.EventReader, error) {
		return decodeResponseBuffered(ctx, doc.RawBytes(), exchangeID, sink)
	})
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.WireStream, exchangeID string) (exchange.Result[canonical.EventReader], error) {
	if err := core.ValidateResponseSSECarrierStream(stream, protocolkind.Completions); err != nil {
		carrierErr := canonical.InternalError("completions stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return exchange.Result[canonical.EventReader]{Value: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (canonical.EventReader, error) {
		return decodeResponseStream(stream, exchangeID, sink), nil
	})
}
