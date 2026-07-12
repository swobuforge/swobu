package exchangeruntime

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
)

// ProviderRequestPath centralizes provider request path selection for the
// shared wire-family codec seam so provider adapters do not each own the same
// protocol switch.
func ProviderRequestPath(providerSpec string, kind protocolkind.ProtocolKind) (string, error) {
	providerID, ok := profile.ParseProviderID(providerSpec)
	if !ok {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	switch providerID {
	case profile.ProviderSpecBedrock:
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
