package runtime

import (
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// Registry is the provider namespace facet lookup surface.
type Registry interface {
	Manifest(providerID profile.ProviderID) (profile.Profile, bool)
	Discovery(providerID profile.ProviderID) (Discovery, bool)
	BackendResolver(providerID profile.ProviderID) (provider.BackendResolver, bool)
}
