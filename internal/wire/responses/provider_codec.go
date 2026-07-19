package responses

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func (ProviderRequestDocumentEncoder) EncodeProviderRequestDocument(input wire.ProviderEncodeInput, d delivery.Delivery, exchangeID string) (wire.ProviderEncodeResult, error) {
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (carrier.Document, error) {
		return EncodeCarrierWithDecisions(EncodeInput{Request: input.Request, NativeContinuation: input.NativeContinuation}, d, sink, exchangeID, EncodeOptions{})
	})
	return wire.ProviderEncodeResult{Document: document, Decisions: decisions}, err
}

func (ProviderRequestDocumentEncoder) EncodeProviderRequestWithOptions(input wire.ProviderEncodeInput, d delivery.Delivery, exchangeID string, options EncodeOptions) (wire.ProviderEncodeResult, error) {
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (carrier.Document, error) {
		return EncodeCarrierWithDecisions(EncodeInput{Request: input.Request, NativeContinuation: input.NativeContinuation}, d, sink, exchangeID, options)
	})
	return wire.ProviderEncodeResult{Document: document, Decisions: decisions}, err
}

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, doc carrier.Document, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseDocument(doc, protocolkind.Responses); err != nil {
		carrierErr := canonical.InternalError("responses response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	stream, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (canonical.ResponseStream, error) {
		return decodeResponseBuffered(ctx, doc.RawBytes(), exchangeID, sink)
	})
	var identity responseEnvelope
	_ = json.Unmarshal(doc.RawBytes(), &identity)
	continuation := &staticResponsesContinuation{id: provider.ContinuationID(strings.TrimSpace(identity.ID))}
	return wire.ProviderDecodeResult{Stream: stream, Decisions: decisions, Continuation: continuation}, err
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.ByteStream, exchangeID string) (wire.ProviderDecodeResult, error) {
	if err := core.ValidateResponseSSEByteStream(stream); err != nil {
		carrierErr := canonical.InternalError("responses stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return wire.ProviderDecodeResult{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	reader, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (*responsesEventReader, error) {
		return decodeResponseStream(stream, exchangeID, sink), nil
	})
	return wire.ProviderDecodeResult{Stream: reader, Decisions: decisions, Continuation: reader, TerminalDecisions: reader}, err
}

type staticResponsesContinuation struct{ id provider.ContinuationID }

func (s *staticResponsesContinuation) ContinuationID() (provider.ContinuationID, bool) {
	return s.id, strings.TrimSpace(string(s.id)) != ""
}
