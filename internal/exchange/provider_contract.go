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

// ProviderModelCatalog reads operator-support model catalogs for one selected
// provider target. It is separate from protocol-path semantic execution.
type ProviderModelCatalog interface {
	ValidateCredentials(ctx context.Context, target RoutableTarget) error
	ListDeployments(ctx context.Context, target RoutableTarget) ([]profile.ProviderDeploymentRecord, error)
}
