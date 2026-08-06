package profile

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ProviderRequestPath resolves the HTTP subpath for a provider and protocol
// combination. It is the single source of truth for provider-edge request path
// selection so provider adapters do not each own the same protocol switch. The
// path is the bare operation (the API namespace is part of the authored base
// URL); every provider that runs over an OpenAI-compatible surface uses the
// same three operations.
func ProviderRequestPath(providerSpec string, kind protocolkind.ProtocolKind) (string, error) {
	if _, ok := ParseProviderID(providerSpec); !ok {
		return "", fmt.Errorf("provider id is unsupported")
	}
	switch kind {
	case protocolkind.ChatCompletions:
		return "/chat/completions", nil
	case protocolkind.Responses:
		return "/responses", nil
	case protocolkind.Messages:
		return "/messages", nil
	default:
		return "", fmt.Errorf("target protocol kind %q has no request path", kind)
	}
}
