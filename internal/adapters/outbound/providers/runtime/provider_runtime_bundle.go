package runtime

import (
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// Discovery is the provider-target discovery facet. It validates credentials
// and lists deployments for one provider target.
type Discovery = provider.Discovery

// ProviderRuntimeBundle groups one provider's runtime facets.
type ProviderRuntimeBundle struct {
	ProviderID         profile.ProviderID
	BackendResolver    provider.BackendResolver
	TargetSupport      provider.TargetSupportResolver
	CredentialProvider CredentialProvider
	Discovery          Discovery
}
