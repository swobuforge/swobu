package provider

import (
	"context"

	"github.com/swobuforge/swobu/internal/profile"
)

// TargetProbeResult keeps provider-specific authoring diagnostics opaque to
// generic dispatch and transport layers. Diagnostics is optional provider-
// owned JSON interpreted only by that provider's operator surface.
type TargetProbeResult struct {
	Deployments []profile.ProviderDeploymentRecord
	Diagnostics []byte
}

// Discovery performs one truthful provider-owned catalog probe.
type Discovery interface {
	ProbeTarget(context.Context, TargetSnapshot) (TargetProbeResult, error)
}
