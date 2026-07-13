package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/exchange"
)

// ProviderModelCatalog reads operator-support model catalogs for one selected
// provider target. It is separate from protocol-path semantic execution.
type ProviderModelCatalog interface {
	ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error
	ListDeployments(ctx context.Context, target exchange.RoutableTarget) ([]ProviderDeploymentRecord, error)
}
