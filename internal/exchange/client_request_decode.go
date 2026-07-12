package exchange

import (
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ClientRequestDecode carries the canonical request plus resolved delivery
// returned by client-family request decoders.
type ClientRequestDecode struct {
	Request  canonical.CanonicalRequest
	Delivery delivery.Delivery
}
