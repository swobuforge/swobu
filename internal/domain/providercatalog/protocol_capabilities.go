package providercatalog

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func SupportedExecutionProtocolsForSpec(spec string) []protocolkind.ProtocolKind {
	if !SupportsSpec(spec) {
		return nil
	}
	switch strings.TrimSpace(strings.ToLower(spec)) { // trimlowerlint:allow domain canonicalization
	case "anthropic":
		return []protocolkind.ProtocolKind{protocolkind.Messages}
	default:
		return []protocolkind.ProtocolKind{
			protocolkind.ChatCompletions,
			protocolkind.Responses,
			protocolkind.Completions,
		}
	}
}

func SupportsExecutionProtocolForSpec(spec string, protocolKind protocolkind.ProtocolKind) bool {
	for _, supported := range SupportedExecutionProtocolsForSpec(spec) {
		if supported == protocolKind {
			return true
		}
	}
	return false
}

func DefaultExecutionProtocolForSpec(spec string) (protocolkind.ProtocolKind, bool) {
	supported := SupportedExecutionProtocolsForSpec(spec)
	if len(supported) == 0 {
		return "", false
	}
	return supported[0], true
}
