package runtime

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

// CredentialProvider resolves credential references into provider tokens.
type CredentialProvider interface {
	ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error)
}

// Executor dispatches one canonical request to a backend provider.
type Executor interface {
	Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error)
}

// ModelCatalogClient lists backend model IDs for one provider target.
type ModelCatalogClient interface {
	ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error)
}

// ProviderRuntime groups one provider's runtime roles.
type ProviderRuntime struct {
	ProviderID         providercatalog.ProviderID
	Executor           Executor
	CredentialProvider CredentialProvider
	ModelCatalogClient ModelCatalogClient
}
