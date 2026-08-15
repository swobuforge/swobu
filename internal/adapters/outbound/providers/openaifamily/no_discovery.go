package openaifamily

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// noDiscovery is the shared closed facet for profiles whose model authoring
// is manual. It prevents an unnecessary network probe while leaving exact
// model identities valid in the inference path.
type noDiscovery struct{}

func (noDiscovery) ProbeTarget(context.Context, provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	return provider.TargetProbeResult{}, canonical.NotImplemented("provider model discovery is not implemented")
}

var _ provider.Discovery = noDiscovery{}
