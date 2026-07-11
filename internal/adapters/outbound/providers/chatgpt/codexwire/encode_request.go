package codexwire

import (
	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

const codexDefaultInstructions = "You are a helpful assistant."

func EncodeProviderRequest(request canonical.CanonicalRequest, _ delivery.Delivery) (carrier.WireDocument, error) {
	// Codex execute path is stream-native; batch clients are handled via
	// stream->batch projection outside this protocol encoder.
	store := false
	codec, err := protocolregistry.ForProviderRequestProtocolCarrier(protocolkind.Responses)
	if err != nil {
		return carrier.WireDocument{}, err
	}
	withOptions, ok := codec.(protocolregistry.ResponsesEncodeOptionsCarrierCodec)
	if !ok {
		return carrier.WireDocument{}, canonical.UnsupportedOperation("responses carrier codec does not support options-based encode")
	}
	return withOptions.EncodeProviderRequestWithOptions(request, delivery.StreamingDelivery(delivery.FramingSSE), protocolregistry.ResponsesEncodeOptions{
		Instructions:         codexDefaultInstructions,
		ForceStructuredInput: true,
		Store:                &store,
	})
}
