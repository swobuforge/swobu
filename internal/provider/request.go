package provider

import (
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/responsesnative"
)

// Request contains only the provider-facing input for one provider call.
// Canonical is either complete semantic state or an exact-target delta whose
// previous-response native handle was already validated by session resolution.
// Responses is independent replay transcript state beside that semantic value.
type Request struct {
	// ExchangeID correlates progressive response events for this invocation. It
	// is execution context, not part of canonical request semantics.
	ExchangeID string
	Canonical  canonical.CanonicalRequest
	// Responses is the independent protocol-native replay refinement. Only a
	// Responses codec consumes it; other protocol families leave it zero.
	Responses      responsesnative.RequestState
	Delivery       delivery.Delivery
	Compatibility  compat.CompatibilityPolicy
	ToolProjection ToolProjectionTable
}
