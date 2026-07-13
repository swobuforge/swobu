package runtime

import (
	"github.com/swobuforge/swobu/internal/profile"
)

// Registry is the provider namespace facet lookup surface.
type Registry interface {
	Manifest(providerID profile.ProviderID) (profile.Profile, bool)
	Discovery(providerID profile.ProviderID) (Discovery, bool)
	Ingress(providerID profile.ProviderID) (ProviderExecutor, bool)
}
