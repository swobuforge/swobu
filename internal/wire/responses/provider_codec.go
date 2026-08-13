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
	document, err := EncodeCarrierWithChanges(EncodeInput{Request: input.Request, PreviousHistory: input.PreviousHistory, ToolNames: input.ToolNames}, d, &changes, exchangeID, EncodeOptions{})
	return wire.ProviderEncodeResult{Document: document, Changes: changes}, err
}

func (ProviderRequestDocumentEncoder) EncodeProviderRequestWithOptions(input wire.ProviderEncodeInput, d delivery.Delivery, exchangeID string, options EncodeOptions) (wire.ProviderEncodeResult, error) {
	var changes []compat.Change
	document, err := EncodeCarrierWithChanges(EncodeInput{Request: input.Request, PreviousHistory: input.PreviousHistory, ToolNames: input.ToolNames}, d, &changes, exchangeID, options)
	return wire.ProviderEncodeResult{Document: document, Changes: changes}, err
}

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, request canonical.CanonicalRequest, names wire.ToolNames, doc carrier.Document, exchangeID string) (wire.ProviderDecodeResult, error) {
	return (ProviderDocumentDecoder{}).DecodeProviderDocumentWithCapture(ctx, request, names, doc, exchangeID, false)
}

// DecodeProviderDocumentWithCapture lowers one provider response document. A
// provider response ID is captured as reusable ResponsesContinuation only when
// the exact provider codec and request permit continuation and the response
// explicitly confirms store:true. Otherwise it stays identity-only and the
// request path falls back to full materialized history.
func (ProviderDocumentDecoder) DecodeProviderDocumentWithCapture(ctx context.Context, request canonical.CanonicalRequest, names wire.ToolNames, doc carrier.Document, exchangeID string, continuationEligible bool) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseDocument(doc, protocolkind.Responses); err != nil {
		carrierErr := canonical.InternalError("responses response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	var changes []compat.Change
	stream, err := decodeResponseBuffered(ctx, request, names, doc.RawBytes(), exchangeID, &changes, continuationEligible)
	return wire.ProviderDecodeResult{Stream: stream, Changes: changes}, err
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(request canonical.CanonicalRequest, names wire.ToolNames, stream carrier.ByteStream, exchangeID string) (wire.ProviderDecodeResult, error) {
	return (ProviderEnvelopeDecoder{}).DecodeProviderEnvelopeWithCapture(request, names, stream, exchangeID, false)
}

// DecodeProviderEnvelopeWithCapture lowers one provider response stream. A
// provider response ID is captured as reusable ResponsesContinuation only when
// the exact provider codec and request permit continuation and the response
// explicitly confirms store:true. Otherwise it stays identity-only and the
// request path falls back to full materialized history.
func (ProviderEnvelopeDecoder) DecodeProviderEnvelopeWithCapture(request canonical.CanonicalRequest, names wire.ToolNames, stream carrier.ByteStream, exchangeID string, continuationEligible bool) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseSSEByteStream(stream); err != nil {
		carrierErr := canonical.InternalError("responses stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	var changes []compat.Change
	reader := decodeResponseStream(request, names, stream, exchangeID, &changes, continuationEligible)
	return wire.ProviderDecodeResult{Stream: reader, Changes: changes, ProgressiveChanges: reader.Changes}, nil
}
