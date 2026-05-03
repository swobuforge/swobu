package requestpath

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/ports"
)

// ClientIntent is immutable client-origin request meaning at requestpath ingress.
type ClientIntent struct {
	EndpointName   endpointintent.EndpointName
	RequestID      string
	Request        compatibility.CanonicalRequest
	RequestedModel string
	Provenance     IngressProvenance
}

// RouteDecision is one concrete route choice for one execution attempt.
type RouteDecision struct {
	Target         ports.RoutableTarget
	EffectiveModel string
	ResolutionMode string
	Reason         string
}

// ExecutionAttempt is the executable unit for one provider execution try.
type ExecutionAttempt struct {
	Intent       ClientIntent
	Route        RouteDecision
	Index        int
	Contract     ExecutionContract
	Request      compatibility.CanonicalRequest
	Capabilities CapabilitySnapshot
	Continuation compatibility.ContinuationRuntime
}

// AttemptOutcome is one attempt result consumed by requestpath policy middleware.
type AttemptOutcome struct {
	Response ports.ExecuteResponse
	Err      error
}

type RoutingPolicy interface {
	Decide(ctx context.Context, endpoint endpointintent.Endpoint, intent ClientIntent) (RouteDecision, error)
}

type AttemptPipeline interface {
	Execute(ctx context.Context, attempt ExecutionAttempt) AttemptOutcome
}
