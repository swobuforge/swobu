package responses

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func EncodeCarrier(request canonical.CanonicalRequest, d delivery.Delivery) (carrier.Document, error) {
	return EncodeCarrierWithDecisions(EncodeInput{Request: request}, d, nil, "", EncodeOptions{})
}
