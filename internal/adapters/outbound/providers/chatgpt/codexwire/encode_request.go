package codexwire

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/wire"
	responses "github.com/swobuforge/swobu/internal/wire/responses"
)

func EncodeProviderRequestDocument(request canonical.CanonicalRequest, _ delivery.Delivery, exchangeID string) (exchange.Result[carrier.CarrierDocument], error) {
	// Codex execute path is stream-native; batch clients are handled via
	// stream->batch projection outside this protocol encoder. No extra prompt
	// overlay is injected here; the adapter only preserves the required wire
	// shape for the execute route.
	store := false
	return responses.ProviderRequestDocumentEncoder{}.EncodeProviderRequestWithOptions(wire.ProviderEncodeInput{Request: request}, delivery.StreamingDelivery(delivery.FramingSSE), exchangeID, responses.EncodeOptions{
		ForceStructuredInput: true,
		Store:                &store,
	})
}
