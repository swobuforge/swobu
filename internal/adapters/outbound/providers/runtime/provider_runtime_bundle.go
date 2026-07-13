package runtime

import (
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

// ProviderExecutor is the provider-edge execution facet. It turns one resolved
// provider request into provider ingress without owning exchange orchestration.
type ProviderExecutor = ports.ProviderIngressResolver

// Discovery is the provider-target discovery facet. It validates credentials
// and lists deployments for one provider target.
type Discovery = ports.ProviderModelCatalog

// ProviderRuntimeBundle groups one provider's runtime facets.
type ProviderRuntimeBundle struct {
	ProviderID         profile.ProviderID
	ProviderExecutor   ProviderExecutor
	CredentialProvider CredentialProvider
	Discovery          Discovery
}
