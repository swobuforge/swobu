package messages

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/wire"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func (ProviderRequestDocumentEncoder) EncodeProviderRequestDocument(input wire.ProviderEncodeInput, delivery delivery.Delivery, exchangeID string) (effect.Result[carrier.CarrierDocument], error) {
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (carrier.CarrierDocument, error) {
		return EncodeCarrierWithEffects(input.Request, delivery, sink, exchangeID)
	})
}

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, doc carrier.CarrierDocument, exchangeID string) (effect.Result[canonical.EventReader], error) {
	if err := core.ValidateResponseCarrierDocument(doc, protocolkind.Messages); err != nil {
		carrierErr := canonical.InternalError("messages response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return effect.Result[canonical.EventReader]{Value: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (canonical.EventReader, error) {
		return decodeResponseBuffered(ctx, doc.RawBytes(), exchangeID, sink)
	})
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.CarrierStream, exchangeID string) (effect.Result[canonical.EventReader], error) {
	if err := core.ValidateResponseSSECarrierStream(stream, protocolkind.Messages); err != nil {
		carrierErr := canonical.InternalError("messages stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return effect.Result[canonical.EventReader]{Value: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (canonical.EventReader, error) {
		return decodeResponseStream(stream, exchangeID, sink), nil
	})
}
