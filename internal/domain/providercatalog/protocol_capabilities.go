package providercatalog

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func SupportedExecutionProtocolsForSpec(spec string) []protocolkind.ProtocolKind {
	if !SupportsSpec(spec) {
		return nil
	}
	normalizedSpec := strings.TrimSpace(strings.ToLower(spec)) // swobu:io-string source=provider-config
	switch normalizedSpec {
	case "anthropic":
		return []protocolkind.ProtocolKind{protocolkind.Messages}
	case "chatgpt":
		return []protocolkind.ProtocolKind{
			protocolkind.ChatCompletions,
			protocolkind.Responses,
		}
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
