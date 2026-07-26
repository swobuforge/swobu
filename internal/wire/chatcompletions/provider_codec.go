package chatcompletions

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

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, request canonical.CanonicalRequest, doc carrier.Document, exchangeID string) (wire.ProviderDecodeResult, error) {
	return (ProviderDocumentDecoder{}).DecodeProviderDocumentWithOptions(ctx, request, doc, exchangeID)
}

func (ProviderDocumentDecoder) DecodeProviderDocumentWithOptions(ctx context.Context, request canonical.CanonicalRequest, doc carrier.Document, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseDocument(doc, protocolkind.ChatCompletions); err != nil {
		carrierErr := canonical.InternalError("chat completions response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	stream, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (canonical.ResponseStream, error) {
		return decodeResponseBuffered(ctx, request, doc.RawBytes(), exchangeID, sink)
	})
	return wire.ProviderDecodeResult{Stream: stream, Decisions: decisions}, err
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(request canonical.CanonicalRequest, stream carrier.ByteStream, exchangeID string) (wire.ProviderDecodeResult, error) {
	return (ProviderEnvelopeDecoder{}).DecodeProviderEnvelopeWithOptions(request, stream, exchangeID)
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelopeWithOptions(request canonical.CanonicalRequest, stream carrier.ByteStream, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseSSEByteStream(stream); err != nil {
		carrierErr := canonical.InternalError("chat completions stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	reader, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (*chatCompletionsEventReader, error) {
		return decodeResponseStream(request, stream, exchangeID, sink), nil
	})
	return wire.ProviderDecodeResult{Stream: reader, Decisions: decisions, TerminalDecisions: reader}, err
}
