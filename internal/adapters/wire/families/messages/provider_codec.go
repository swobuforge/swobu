package messages

import (
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func (ProviderRequestDocumentEncoder) EncodeProviderRequestDocument(request canonical.CanonicalRequest, delivery delivery.Delivery) (carrier.WireDocument, error) {
	return EncodeCarrier(request, delivery)
}

func (ProviderDocumentDecoder) DecodeProviderDocument(doc carrier.WireDocument, exchangeID string) (canonical.EventReader, error) {
	if err := core.ValidateResponseCarrierDocument(doc, protocolkind.Messages); err != nil {
		carrierErr := canonical.InternalError("messages response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return nil, carrierErr
	}
	return decodeResponseBuffered(doc.RawBytes(), exchangeID)
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.WireStream, exchangeID string) canonical.EventReader {
	if err := core.ValidateResponseSSECarrierStream(stream, protocolkind.Messages); err != nil {
		carrierErr := canonical.InternalError("messages stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return canonical.NewErrorEventReader(carrierErr)
	}
	return decodeResponseStream(stream, exchangeID)
}
