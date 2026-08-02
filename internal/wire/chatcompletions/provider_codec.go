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
)

func (ProviderRequestDocumentEncoder) EncodeProviderRequestDocument(input wire.ProviderEncodeInput, delivery delivery.Delivery, exchangeID string) (wire.ProviderEncodeResult, error) {
	var changes []compat.Change
	document, err := EncodeCarrierWithChanges(input.Request, input.ToolNames, delivery, &changes, exchangeID)
	return wire.ProviderEncodeResult{Document: document, Changes: changes}, err
}

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, request canonical.CanonicalRequest, names wire.ToolNames, doc carrier.Document, exchangeID string) (wire.ProviderDecodeResult, error) {
	return (ProviderDocumentDecoder{}).DecodeProviderDocumentWithOptions(ctx, request, names, doc, exchangeID)
}

func (ProviderDocumentDecoder) DecodeProviderDocumentWithOptions(ctx context.Context, request canonical.CanonicalRequest, names wire.ToolNames, doc carrier.Document, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseDocument(doc, protocolkind.ChatCompletions); err != nil {
		carrierErr := canonical.InternalError("chat completions response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	var changes []compat.Change
	stream, err := decodeResponseBuffered(ctx, request, names, doc.RawBytes(), exchangeID, &changes)
	return wire.ProviderDecodeResult{Stream: stream, Changes: changes}, err
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(request canonical.CanonicalRequest, names wire.ToolNames, stream carrier.ByteStream, exchangeID string) (wire.ProviderDecodeResult, error) {
	return (ProviderEnvelopeDecoder{}).DecodeProviderEnvelopeWithOptions(request, names, stream, exchangeID)
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelopeWithOptions(request canonical.CanonicalRequest, names wire.ToolNames, stream carrier.ByteStream, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseSSEByteStream(stream); err != nil {
		carrierErr := canonical.InternalError("chat completions stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	var changes []compat.Change
	reader := decodeResponseStream(request, names, stream, exchangeID, &changes)
	return wire.ProviderDecodeResult{Stream: reader, Changes: changes, ProgressiveChanges: reader.Changes}, nil
}
