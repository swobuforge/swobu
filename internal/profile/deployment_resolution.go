package profile

import "strings"

// ModelAuthoringResolution interprets one advisory model-authoring option
// against the provider spec that surfaced it. It keeps discovery facts
// separate from selection policy so callers can resolve lazily when they
// actually need a concrete protocol answer.
type ModelAuthoringResolution struct {
	providerSpec string
	option       ModelAuthoringOption
}

// ResolveModelAuthoringOption returns one conservative resolver for one
// model-authoring option under one provider spec.
func ResolveModelAuthoringOption(providerSpec string, option ModelAuthoringOption) ModelAuthoringResolution {
	return ModelAuthoringResolution{
		providerSpec: strings.TrimSpace(providerSpec), // swobu:io-string source=boundary
		option:       option,
	}
}

// ProtocolOptions returns the concrete protocols available for the option.
// Explicit option metadata wins; sparse option metadata inherits the
// provider manifest's concrete protocol list.
func (r ModelAuthoringResolution) ProtocolOptions() []string {
	if options := CloneModelIDs(r.option.SupportedProviderProtocols); len(options) > 0 {
		out := make([]string, 0, len(options))
		seen := make(map[string]struct{}, len(options))
		for _, option := range options {
			canonical, err := NormalizeProviderProtocolForSpec(r.providerSpec, option)
			if err != nil || canonical == "" {
				continue
			}
			if _, exists := seen[canonical]; exists {
				continue
			}
			seen[canonical] = struct{}{}
			out = append(out, canonical)
		}
		if len(out) > 0 {
			return out
		}
	}
	return ConcreteProviderProtocolsForSpec(r.providerSpec)
}

// DefaultProtocol returns the explicit option default when present and
// supported. Sparse options do not invent one.
func (r ModelAuthoringResolution) DefaultProtocol() string {
	defaultProtocol := strings.TrimSpace(r.option.DefaultProviderProtocol) // swobu:io-string source=boundary
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
// resolved option set.
func (r ModelAuthoringResolution) SupportsProtocol(protocol string) bool {
	protocol = strings.TrimSpace(protocol) // swobu:io-string source=boundary
	if protocol == "" {
		return false
	}
	for _, supported := range r.ProtocolOptions() {
		if supported == protocol {
			return true
		}
	}
	return false
}
