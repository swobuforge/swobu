package providercatalog

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

const ProviderProtocolAuto = "protocol_auto"

type ProviderProtocol string

func SupportedProviderProtocolsForSpec(spec string) []string {
	profile, ok := profileFor(spec)
	if !ok {
		return nil
	}
	protocols := make([]string, 0, len(profile.ProviderProtocols)+1)
	protocols = append(protocols, ProviderProtocolAuto)
	for _, protocol := range profile.ProviderProtocols {
		protocols = append(protocols, protocol.Name)
	}
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

func DefaultProviderProtocolForSpec(spec string) (string, bool) {
	profile, ok := profileFor(spec)
	if !ok || len(profile.ProviderProtocols) == 0 {
		return "", false
	}
	for _, protocol := range profile.ProviderProtocols {
		if protocol.Frame == FrameSSEEvent {
			return protocol.Name, true
		}
	}
	return profile.ProviderProtocols[0].Name, true
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

// DecodeProviderProtocolFromPersistence normalizes one persisted/operator-wire
// provider protocol token. Empty means "unspecified" and is valid.
//
// protocol_auto is UI-only and must never cross daemon durability/control
// boundaries; callers should fail fast when it appears.
func DecodeProviderProtocolFromPersistence(spec string, providerProtocol string) (string, error) {
	normalized := strings.TrimSpace(providerProtocol) // swobu:io-string source=domain
	if normalized == "" {
		return "", nil
	}
	if normalized == ProviderProtocolAuto {
		return "", fmt.Errorf("provider protocol %q is UI-only and unsupported at daemon boundaries", ProviderProtocolAuto)
	}
	if !SupportsProviderProtocolForSpec(spec, normalized) {
		return "", fmt.Errorf("provider protocol %q is unsupported for provider %q", normalized, spec)
	}
	return normalized, nil
}
