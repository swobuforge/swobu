package responses

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func EncodeCarrier(request canonical.CanonicalRequest, d delivery.Delivery) (carrier.Document, error) {
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeCarrierWithChanges(EncodeInput{Request: request, ToolNames: names}, d, nil, "", EncodeOptions{})
}
