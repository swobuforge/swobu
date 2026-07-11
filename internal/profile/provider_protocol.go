package profile

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

const ProviderProtocolAuto = "auto"

type ProviderProtocol string

// ConcreteProviderProtocolsForSpec returns provider-native concrete protocols in
// the catalog-declared order. "auto" is intentionally excluded.
func ConcreteProviderProtocolsForSpec(spec string) []string {
	profile, ok := profileFor(spec)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(profile.ProviderProtocols))
	for _, protocol := range profile.ProviderProtocols {
		name := strings.TrimSpace(protocol.Name) // swobu:io-string source=domain
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

func SupportedProviderProtocolsForSpec(spec string) []string {
	concrete := ConcreteProviderProtocolsForSpec(spec)
	if len(concrete) == 0 {
		return nil
	}
	protocols := make([]string, 0, len(concrete)+1)
	protocols = append(protocols, ProviderProtocolAuto)
	protocols = append(protocols, concrete...)
	return protocols
}

func SupportsProviderProtocolForSpec(spec string, providerProtocol string) bool {
	for _, supported := range SupportedProviderProtocolsForSpec(spec) {
		if supported == providerProtocol {
			return true
		}
	}
	return false
}

// ResolveConcreteProtocolForAutoAtBoundary chooses one concrete protocol for a
// provider when callers require a concrete fallback value.
func ResolveConcreteProtocolForAutoAtBoundary(spec string) (string, bool) {
	concrete := ConcreteProviderProtocolsForSpec(spec)
	if len(concrete) == 0 {
		return "", false
	}
	return concrete[0], true
}

func ProviderProtocolKindAndFrame(spec string, providerProtocol string) (protocolkind.ProtocolKind, string, bool) {
	profile, ok := profileFor(spec)
	if !ok {
		return "", "", false
	}
	for _, supported := range profile.ProviderProtocols {
		if supported.Name == providerProtocol {
			return supported.Kind, supported.Frame, true
		}
	}
	return "", "", false
}

func ConcreteProviderProtocolForSpecKindFrame(spec string, kind protocolkind.ProtocolKind, frame string) (string, bool) {
	profile, ok := profileFor(spec)
	if !ok {
		return "", false
	}
	for _, protocol := range profile.ProviderProtocols {
		if protocol.Kind == kind && protocol.Frame == frame {
			return protocol.Name, true
		}
	}
	return "", false
}

func EncodeProviderProtocolForPersistence(providerProtocol string) string {
	normalized := strings.TrimSpace(providerProtocol) // swobu:io-string source=domain
	if normalized == "" || normalized == ProviderProtocolAuto {
		return ""
	}
	return normalized
}

// DecodeProviderProtocolFromPersistence normalizes one persisted provider
// protocol token. Empty means "unspecified" and is valid.
func DecodeProviderProtocolFromPersistence(spec string, providerProtocol string) (string, error) {
	normalized := strings.TrimSpace(providerProtocol) // swobu:io-string source=domain
	if normalized == "" {
		return "", nil
	}
	if normalized == ProviderProtocolAuto {
		return "", fmt.Errorf("provider protocol %q is not allowed in persisted config", ProviderProtocolAuto)
	}
	if !SupportsProviderProtocolForSpec(spec, normalized) {
		return "", fmt.Errorf("provider protocol %q is unsupported for provider %q", normalized, spec)
	}
	return normalized, nil
}
