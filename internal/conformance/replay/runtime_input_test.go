package replay

import (
	"github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
)

func withRuntimeInput(in exchange.ClientInput) exchange.ClientInput {
	req, _ := protocolregistry.ForClientRequestDecoder(in.ClientFamily)
	doc, _ := protocolregistry.ForClientDocumentEncoder(in.ClientFamily)
	stream, _ := protocolregistry.ForClientStreamEncoder(in.ClientFamily)
	providerEncoder, _ := protocolregistry.ForProviderRequestProtocolCarrier(in.ProviderFamily)
	in.ClientCodec = replayClientCodec{req: req, doc: doc, stream: stream}
	in.ProviderEncoder = providerEncoder
	if in.ProviderDelivery.Mode == delivery.Streaming {
		streamDecoder, _ := protocolregistry.ForProviderResponseStreamProtocolCarrier(in.ProviderFamily)
		in.StreamDecoder = streamDecoder
	}
	if in.ProviderDelivery.Mode == delivery.Buffered {
		docDecoder, _ := protocolregistry.ForProviderResponseDocumentProtocolCarrierEnvelope(in.ProviderFamily)
		in.DocumentDecoder = docDecoder
	}
	return in
}

type replayClientCodec struct {
	req    protocolregistry.ClientRequestDecoder
	doc    protocolregistry.ClientDocumentEncoder
	stream protocolregistry.ClientStreamEncoder
}

func (c replayClientCodec) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	return c.req.DecodeClientRequest(doc)
}

func (c replayClientCodec) EncodeClientDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	return c.doc.EncodeClientDocument(output)
}

func (c replayClientCodec) EncodeClientStream(events canonical.EventReader) (carrier.WireStream, error) {
	return c.stream.EncodeClientStream(events)
}
