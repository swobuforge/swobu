// Package exchange owns the request path: ingress, routing, provider contract, and execution.
//
// Sub-packages:
//   - stage: pipeline stage types
//   - machine, turnstate, observation: called by this package, not callers.
//
// Laws:
//   - Does not import adapters.
//   - Routing policy lives in internal/routing; this package only executes.
package exchange

import (
	"context"

	"github.com/swobuforge/swobu/internal/profile"
)

// TargetProbeResult keeps provider-specific authoring diagnostics opaque to
// generic dispatch and transport layers. Diagnostics is an optional JSON
// object owned and decoded only by the selected provider's operator surface.
type TargetProbeResult struct {
	Deployments []profile.ProviderDeploymentRecord
	Diagnostics []byte
}

// ProviderDiscovery performs one truthful provider-owned catalog probe.
type ProviderDiscovery interface {
	ProbeTarget(ctx context.Context, target RoutableTarget) (TargetProbeResult, error)
}
