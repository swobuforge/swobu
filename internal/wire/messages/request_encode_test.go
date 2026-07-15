package messages

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.CarrierDocument, error) {
	return EncodeCarrierWithEffects(req, d, nil, "")
}
