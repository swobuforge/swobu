package provider

import (
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// Request contains only the provider-facing input for one provider call.
// Canonical is either complete semantic state or an exact-target delta whose
// previous-response refinement was already validated by replay preparation.
type Request struct {
	Canonical canonical.CanonicalRequest
	Delivery  delivery.Delivery
}
