package provider

import (
	"context"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// EncodeContext carries request-scoped capabilities that an exact provider
// codec may invoke while lowering canonical input. URL-native codecs leave
// ResolveImage untouched; byte-only codecs call it only for URL images.
type EncodeContext struct {
	Context      context.Context
	ResolveImage func(context.Context, canonical.URLImage) (InspectedImage, error)
	// HasNextRouteCandidate is transient exchange execution context. It is not
	// canonical intent, target capability, or persisted routing configuration.
	HasNextRouteCandidate bool
}

// PreviousHistory authorizes one exact provider codec to replace a contiguous
// complete-request history range with its typed native continuation handle.
// The closed ResponseRef remains the handle authority; no provider-generic ID
// or metadata map can be introduced at this seam.
type PreviousHistory struct {
	Response  canonical.ResponseRef
	OmitStart uint32
	OmitEnd   uint32
}

// Request contains only the provider-facing input for one provider call.
// Canonical is always the complete effective canonical request and is the only
// request-history authority. PreviousHistory is optional exact-target lowering
// data; it never changes Canonical's meaning.
type Request struct {
	// ExchangeID correlates progressive response events for this invocation. It
	// is execution context, not part of canonical request semantics.
	ExchangeID string
	Canonical  canonical.CanonicalRequest
	// TargetSupport is the immutable knowledge snapshot resolved for this exact
	// attempt. Feature owners decide how Unknown affects their own behavior.
	TargetSupport   TargetSupport
	PreviousHistory *PreviousHistory
	EncodeContext   EncodeContext
	Delivery        delivery.Delivery
	// ToolNames is transient provider-attempt representation state.
	ToolNames AttemptToolNames
}
