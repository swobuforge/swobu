package profile

import (
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func SupportedExecutionProtocolsForSpec(spec string) []protocolkind.ProtocolKind {
	profile, ok := profileFor(spec)
	if !ok {
		return nil
	}
	supported := make([]protocolkind.ProtocolKind, 0, len(profile.ProviderProtocols))
	seen := map[protocolkind.ProtocolKind]bool{}
	for _, protocol := range profile.ProviderProtocols {
		if seen[protocol.Kind] {
			continue
		}
		seen[protocol.Kind] = true
		supported = append(supported, protocol.Kind)
	}
	return supported
}

func SupportsExecutionProtocolForSpec(spec string, protocolKind protocolkind.ProtocolKind) bool {
	for _, supported := range SupportedExecutionProtocolsForSpec(spec) {
		if supported == protocolKind {
			return true
		}
	}
	return false
}
