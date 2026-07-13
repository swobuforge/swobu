package chatcompletions

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func (ProviderRequestDocumentEncoder) EncodeProviderRequestDocument(request canonical.CanonicalRequest, delivery delivery.Delivery, exchangeID string) (effect.Result[carrier.WireDocument], error) {
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (carrier.WireDocument, error) {
		return EncodeCarrierWithEffects(request, delivery, sink, exchangeID)
	})
}

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, doc carrier.WireDocument, exchangeID string) (effect.Result[canonical.EventReader], error) {
	if err := core.ValidateResponseCarrierDocument(doc, protocolkind.ChatCompletions); err != nil {
		carrierErr := canonical.InternalError("chat completions response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return effect.Result[canonical.EventReader]{Value: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (canonical.EventReader, error) {
		return decodeResponseBuffered(ctx, doc.RawBytes(), exchangeID, sink)
	})
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.WireStream, exchangeID string) (effect.Result[canonical.EventReader], error) {
	if err := core.ValidateResponseSSECarrierStream(stream, protocolkind.ChatCompletions); err != nil {
		carrierErr := canonical.InternalError("chat completions stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return effect.Result[canonical.EventReader]{Value: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (canonical.EventReader, error) {
		return decodeResponseStream(stream, exchangeID, sink), nil
	})
}
