package responses

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

func (ProviderRequestDocumentEncoder) EncodeProviderRequestDocument(input wire.ProviderEncodeInput, d delivery.Delivery, exchangeID string) (wire.ProviderEncodeResult, error) {
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (carrier.Document, error) {
		return EncodeCarrierWithDecisions(EncodeInput{Request: input.Request}, d, sink, exchangeID, EncodeOptions{})
	})
	return wire.ProviderEncodeResult{Document: document, Decisions: decisions}, err
}

func (ProviderRequestDocumentEncoder) EncodeProviderRequestWithOptions(input wire.ProviderEncodeInput, d delivery.Delivery, exchangeID string, options EncodeOptions) (wire.ProviderEncodeResult, error) {
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (carrier.Document, error) {
		return EncodeCarrierWithDecisions(EncodeInput{Request: input.Request}, d, sink, exchangeID, options)
	})
	return wire.ProviderEncodeResult{Document: document, Decisions: decisions}, err
}

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, request canonical.CanonicalRequest, doc carrier.Document, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseDocument(doc, protocolkind.Responses); err != nil {
		carrierErr := canonical.InternalError("responses response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	stream, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (canonical.ResponseStream, error) {
		return decodeResponseBuffered(ctx, request, doc.RawBytes(), exchangeID, sink)
	})
	return wire.ProviderDecodeResult{Stream: stream, Decisions: decisions}, err
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(request canonical.CanonicalRequest, stream carrier.ByteStream, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseSSEByteStream(stream); err != nil {
		carrierErr := canonical.InternalError("responses stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	reader, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (*responsesEventReader, error) {
		return decodeResponseStream(request, stream, exchangeID, sink), nil
	})
	return wire.ProviderDecodeResult{Stream: reader, Decisions: decisions, TerminalDecisions: reader}, err
}
