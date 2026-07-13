package profile

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ProviderRequestPath resolves the HTTP subpath for a provider and protocol
// combination. It is the single source of truth for provider-edge request path
// selection so provider adapters do not each own the same protocol switch.
func ProviderRequestPath(providerSpec string, kind protocolkind.ProtocolKind) (string, error) {
	providerID, ok := ParseProviderID(providerSpec)
	if !ok {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	switch providerID {
	case ProviderSpecBedrock:
		switch kind {
		case protocolkind.ChatCompletions:
			return "/chat/completions", nil
		case protocolkind.Responses:
			return "/responses", nil
		case protocolkind.Messages:
			return "/messages", nil
		default:
			return "", canonical.UnsupportedOperation("bedrock provider does not implement the requested protocol")
		}
	default:
		switch kind {
		case protocolkind.ChatCompletions:
			return "/chat/completions", nil
		case protocolkind.Responses:
			return "/responses", nil
		case protocolkind.Completions:
			return "/completions", nil
		case protocolkind.Messages:
			return "/messages", nil
		default:
			return "", canonical.UnsupportedOperation("protocol kind is not implemented")
		}
	}
}
