package runtime

import (
	"context"

	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

// CredentialProvider resolves credential references into provider tokens.
type CredentialProvider interface {
	ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error)
}

// IngressResolver dispatches one canonical request to a backend provider and
// returns the first truthful ingress carrier for the downstream exchange
// pipeline.
type IngressResolver interface {
	ResolveProviderIngress(ctx context.Context, req ports.ProviderRequest) (ports.ProviderIngress, error)
}

// ModelCatalogClient lists backend model IDs for one provider target.
type ModelCatalogClient interface {
	ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error
	ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error)
}

// ProviderRuntimeBundle groups one provider's runtime roles.
type ProviderRuntimeBundle struct {
	ProviderID         profile.ProviderID
	IngressResolver    IngressResolver
	CredentialProvider CredentialProvider
	ModelCatalogClient ModelCatalogClient
}
