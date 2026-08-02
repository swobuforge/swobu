package messages

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.Document, error) {
	names, _, err := provider.BuildAttemptToolNames(req)
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeCarrierWithChanges(req, names, d, nil, "")
}
