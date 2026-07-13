package ports

import (
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/profile"
)

// ProviderDeploymentResolution interprets one deployment record against the
// provider spec that surfaced it. It keeps discovery facts separate from
// selection policy so callers can resolve lazily when they actually need a
// concrete protocol or capability answer.
type ProviderDeploymentResolution struct {
	providerSpec string
	deployment   ProviderDeploymentRecord
}

// ResolveProviderDeployment returns one conservative resolver for one
// deployment record under one provider spec.
func ResolveProviderDeployment(providerSpec string, deployment ProviderDeploymentRecord) ProviderDeploymentResolution {
	return ProviderDeploymentResolution{
		providerSpec: strings.TrimSpace(providerSpec), // swobu:io-string source=boundary
		deployment:   deployment,
	}
}

// ProtocolOptions returns the concrete protocols available for the deployment.
// Explicit deployment metadata wins; sparse deployment metadata inherits the
// provider manifest's concrete protocol list. Auto is intentionally excluded.
func (r ProviderDeploymentResolution) ProtocolOptions() []string {
	if options := CloneModelIDs(r.deployment.SupportedProviderProtocols); len(options) > 0 {
		out := make([]string, 0, len(options))
		for _, option := range options {
			if option == profile.ProviderProtocolAuto {
				continue
			}
			out = append(out, option)
		}
		if len(out) > 0 {
			return out
		}
	}
	return profile.ConcreteProviderProtocolsForSpec(r.providerSpec)
}

// DefaultProtocol returns the explicit deployment default when present and
// supported. Sparse deployments do not invent one.
func (r ProviderDeploymentResolution) DefaultProtocol() string {
	defaultProtocol := strings.TrimSpace(r.deployment.DefaultProviderProtocol) // swobu:io-string source=boundary
	if defaultProtocol == "" {
		return ""
	}
	for _, protocol := range r.ProtocolOptions() {
		if protocol == defaultProtocol {
			return defaultProtocol
		}
	}
	return ""
}

// SupportsProtocol reports whether one concrete protocol is present in the
// resolved deployment options. Auto is not treated as a deployment protocol.
func (r ProviderDeploymentResolution) SupportsProtocol(protocol string) bool {
	protocol = strings.TrimSpace(protocol) // swobu:io-string source=boundary
	if protocol == "" || protocol == profile.ProviderProtocolAuto {
		return false
	}
	for _, supported := range r.ProtocolOptions() {
		if supported == protocol {
			return true
		}
	}
	return false
}

// CapabilitySupport returns the explicit support observation for one feature.
// Missing facts remain unknown so optional behavior fails closed.
func (r ProviderDeploymentResolution) CapabilitySupport(feature compat.Feature) compat.Support {
	for _, capability := range r.deployment.Capabilities {
		if capability.Feature != feature {
			continue
		}
		support := strings.TrimSpace(string(capability.Support)) // swobu:io-string source=boundary
		switch compat.Support(support) {
		case compat.Supported, compat.Unsupported, compat.Partial, compat.Unknown:
			return compat.Support(support)
		default:
			return compat.Unknown
		}
	}
	return compat.Unknown
}
