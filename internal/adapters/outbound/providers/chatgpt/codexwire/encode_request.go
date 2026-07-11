package codexwire

import (
	responses "github.com/swobuforge/swobu/internal/adapters/wire/families/responses"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func EncodeProviderRequestDocument(request canonical.CanonicalRequest, _ delivery.Delivery) (carrier.WireDocument, error) {
	// Codex execute path is stream-native; batch clients are handled via
	// stream->batch projection outside this protocol encoder. No extra prompt
	// overlay is injected here; the adapter only preserves the required wire
	// shape for the execute route.
	store := false
	return responses.ProviderRequestDocumentEncoder{}.EncodeProviderRequestWithOptions(request, delivery.StreamingDelivery(delivery.FramingSSE), responses.EncodeOptions{
		ForceStructuredInput: true,
		Store:                &store,
	})
}
