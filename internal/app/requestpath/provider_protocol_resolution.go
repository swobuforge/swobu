package requestpath

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func resolveProviderProtocolForRequest(providerSpec string, request canonical.CanonicalRequest) (protocolkind.ProtocolKind, error) {
	if !providercatalog.SupportsSpec(providerSpec) {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	if strings.EqualFold(strings.TrimSpace(providerSpec), "anthropic") { // trimlowerlint:allow boundary canonicalization
		switch request.(type) {
		case canonical.DialogCanonicalRequest, canonical.GenerationCanonicalRequest:
			return protocolkind.Messages, nil
		default:
			return "", canonical.UnsupportedOperation("selected provider does not implement the canonical semantic request")
		}
	}

	switch request.(type) {
	case canonical.DialogCanonicalRequest:
		return protocolkind.ChatCompletions, nil
	case canonical.GenerationCanonicalRequest:
		return protocolkind.Responses, nil
	case canonical.PromptCanonicalRequest:
		return protocolkind.Completions, nil
	default:
		return "", canonical.UnsupportedOperation("selected provider does not implement the canonical semantic request")
	}
}
