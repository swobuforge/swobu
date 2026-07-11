package completions

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
	return decodeResponseBuffered(doc.RawBytes(), exchangeID)
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.WireStream, exchangeID string) canonical.EventReader {
	if err := core.ValidateResponseSSECarrierStream(stream, protocolkind.Completions); err != nil {
		carrierErr := canonical.InternalError("completions stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return canonical.NewErrorEventReader(carrierErr)
	}
	return decodeResponseStream(stream, exchangeID)
}
