package exchange

import (
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ClientRequestResult carries the canonical request plus resolved delivery
// returned by client-family request decoders.
type ClientRequestResult struct {
	Request  canonical.CanonicalRequest
	Delivery delivery.Delivery
}
