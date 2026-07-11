package codexwire

import (
	responses "github.com/swobuforge/swobu/internal/adapters/wire/families/responses"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const codexDefaultInstructions = "You are a helpful assistant."

func EncodeProviderRequestDocument(request canonical.CanonicalRequest, _ delivery.Delivery) (carrier.WireDocument, error) {
	// Codex execute path is stream-native; batch clients are handled via
	// stream->batch projection outside this protocol encoder.
	store := false
	return responses.ProviderRequestDocumentEncoder{}.EncodeProviderRequestWithOptions(request, delivery.StreamingDelivery(delivery.FramingSSE), responses.EncodeOptions{
		Instructions:         codexDefaultInstructions,
		ForceStructuredInput: true,
		Store:                &store,
	})
}
