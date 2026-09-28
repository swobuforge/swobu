package provider

import (
	"context"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/cachelocality"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
)

// EncodeContext carries request-scoped capabilities that an exact provider
// codec may invoke while lowering canonical input. URL-native codecs leave
// ResolveImage untouched; byte-only codecs call it only for URL images.
type EncodeContext struct {
	Context      context.Context
	ResolveImage func(context.Context, canonical.URLImage) (InspectedImage, error)
}

// AttemptContext carries provider-facing execution facts for one attempt.
// CorrelationID preserves protocol continuity; it is never the Swobu-owned
// execution ExchangeID. Canonical semantics and credentials stay outside it.
type AttemptContext struct {
	CorrelationID         string
	ExecutionAffinity     executionaffinity.Key
	CacheLocality         cachelocality.Key
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
	Attempt   AttemptContext
	Canonical canonical.CanonicalRequest
	// TargetFacts is one attempt-private empirical dialect reader. Codecs call
	// only the typed getter at a branch they execute; nil reads as preferred.
	TargetFacts     *TargetFacts
	PreviousHistory *PreviousHistory
	EncodeContext   EncodeContext
	Delivery        delivery.Delivery
	// ToolNames is transient provider-attempt representation state.
	ToolNames AttemptToolNames
}
