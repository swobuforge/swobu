package provider

import (
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ContinuationID is an opaque provider-issued continuation handle.
type ContinuationID string

// NativeContinuation binds one opaque provider handle to the routing target
// version that captured it.
type NativeContinuation struct {
	TargetID      string
	TargetVersion uint64
	ID            ContinuationID
}

// Request contains only the provider-facing input for one provider call.
// When Continuation is nil, Canonical is complete semantic state. Otherwise
// Canonical is the current delta and the continuation was already validated
// against the selected backend by exchange orchestration. Delivery is the
// provider-facing wire intent for this round, never the client-facing delivery
// contract.
type Request struct {
	Canonical    canonical.CanonicalRequest
	Continuation *NativeContinuation
	Delivery     delivery.Delivery
}
