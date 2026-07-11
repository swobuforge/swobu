package exchange

import (
	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// RealizeProviderRequestCarrier encodes one canonical request into one provider
// wire document for the selected concrete protocol and delivery.
func RealizeProviderRequestCarrier(
	req canonical.CanonicalRequest,
	kind protocolkind.ProtocolKind,
	delivery delivery.Delivery,
	messagesUnsupportedMessage string,
) (carrier.WireDocument, error) {
	codec, err := protocolregistry.ForProviderRequestProtocolCarrier(kind)
	if err != nil {
		if kind == protocolkind.Messages {
			return carrier.WireDocument{}, canonical.UnsupportedOperation(messagesUnsupportedMessage)
		}
		return carrier.WireDocument{}, err
	}
	return codec.EncodeProviderRequest(req, delivery)
}
