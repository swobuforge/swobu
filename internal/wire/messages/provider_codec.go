package messages

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func (ProviderRequestDocumentEncoder) EncodeProviderRequestDocument(input wire.ProviderEncodeInput, delivery delivery.Delivery, exchangeID string) (wire.ProviderEncodeResult, error) {
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (carrier.Document, error) {
		return EncodeCarrierWithDecisions(input.Request, delivery, sink, exchangeID)
	})
	return wire.ProviderEncodeResult{Document: document, Decisions: decisions}, err
}

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, doc carrier.Document, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseDocument(doc, protocolkind.Messages); err != nil {
		carrierErr := canonical.InternalError("messages response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	stream, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (canonical.ResponseStream, error) {
		return decodeResponseBuffered(ctx, doc.RawBytes(), exchangeID, sink)
	})
	return wire.ProviderDecodeResult{Stream: stream, Decisions: decisions}, err
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.ByteStream, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseSSEByteStream(stream); err != nil {
		carrierErr := canonical.InternalError("messages stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	reader, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (*messagesEventReader, error) {
		return decodeResponseStream(stream, exchangeID, sink), nil
	})
	return wire.ProviderDecodeResult{Stream: reader, Decisions: decisions, TerminalDecisions: reader}, err
}
