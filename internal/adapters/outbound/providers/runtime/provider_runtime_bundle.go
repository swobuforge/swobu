package runtime

import (
	"context"

	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

// CredentialProvider resolves credential references into provider tokens.
type CredentialProvider interface {
	ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error)
}

// Executor dispatches one canonical request to a backend provider and returns
// raw provider transport success carriers.
type Executor interface {
	Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderTransportResponse, error)
}

// ModelCatalogClient lists backend model IDs for one provider target.
type ModelCatalogClient interface {
	ValidateCredentials(ctx context.Context, target ports.RoutableTarget) error
	ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error)
}

// ProviderRuntimeBundle groups one provider's runtime roles.
type ProviderRuntimeBundle struct {
	ProviderID         profile.ProviderID
	Executor           Executor
	CredentialProvider CredentialProvider
	ModelCatalogClient ModelCatalogClient
}
