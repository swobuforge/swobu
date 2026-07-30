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
)

func (ProviderRequestDocumentEncoder) EncodeProviderRequestDocument(input wire.ProviderEncodeInput, d delivery.Delivery, exchangeID string) (wire.ProviderEncodeResult, error) {
	var changes []compat.Change
	document, err := EncodeCarrierWithChanges(EncodeInput{Request: input.Request, Access: input.MCPAccess}, d, &changes, exchangeID, EncodeOptions{})
	return wire.ProviderEncodeResult{Document: document, Changes: changes}, err
}

func (ProviderRequestDocumentEncoder) EncodeProviderRequestWithOptions(input wire.ProviderEncodeInput, d delivery.Delivery, exchangeID string, options EncodeOptions) (wire.ProviderEncodeResult, error) {
	var changes []compat.Change
	document, err := EncodeCarrierWithChanges(EncodeInput{Request: input.Request, Access: input.MCPAccess}, d, &changes, exchangeID, options)
	return wire.ProviderEncodeResult{Document: document, Changes: changes}, err
}

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, request canonical.CanonicalRequest, doc carrier.Document, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseDocument(doc, protocolkind.Responses); err != nil {
		carrierErr := canonical.InternalError("responses response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	var changes []compat.Change
	stream, err := decodeResponseBuffered(ctx, request, doc.RawBytes(), exchangeID, &changes)
	return wire.ProviderDecodeResult{Stream: stream, Changes: changes}, err
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(request canonical.CanonicalRequest, stream carrier.ByteStream, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseSSEByteStream(stream); err != nil {
		carrierErr := canonical.InternalError("responses stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	var changes []compat.Change
	reader := decodeResponseStream(request, stream, exchangeID, &changes)
	return wire.ProviderDecodeResult{Stream: reader, Changes: changes, ProgressiveChanges: reader.Changes}, nil
}
