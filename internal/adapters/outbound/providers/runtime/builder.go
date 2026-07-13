package runtime

import (
	"github.com/swobuforge/swobu/internal/profile"
)

// Builder composes provider facets into a registry at the provider namespace
// edge.
type Builder interface {
	RegisterManifest(profile.Profile)
	RegisterDiscovery(profile.ProviderID, Discovery)
	RegisterIngress(profile.ProviderID, ProviderExecutor)
	Build() Registry
}
