package canonical

import "github.com/swobuforge/swobu/internal/domain/protocolkind"

type IngressFamily = protocolkind.ProtocolKind

const (
	IngressFamilyChatCompletions IngressFamily = protocolkind.ChatCompletions
	IngressFamilyResponses       IngressFamily = protocolkind.Responses
	IngressFamilyCompletions     IngressFamily = protocolkind.Completions
	IngressFamilyMessages        IngressFamily = protocolkind.Messages
)

func InferFamily(method string, normalizedPath NormalizedPath, hasAnthropicVersion bool) (IngressFamily, error) {
	switch {
	case method == "POST" && normalizedPath == NormalizedPathChatCompletions:
		return IngressFamilyChatCompletions, nil
	case method == "POST" && normalizedPath == NormalizedPathResponses:
		return IngressFamilyResponses, nil
	case method == "POST" && normalizedPath == NormalizedPathCompletions:
		return IngressFamilyCompletions, nil
	case method == "POST" && normalizedPath == NormalizedPathMessages && hasAnthropicVersion:
		return IngressFamilyMessages, nil
	default:
		return "", UnsupportedEndpoint("unsupported or ambiguous ingress family")
	}
}

// ValidateIngressTransport enforces compatibility-route transport law before
// family decoding.
func ValidateIngressTransport(method string, normalizedPath NormalizedPath, websocketUpgrade bool) error {
	if normalizedPath == NormalizedPathModels {
		return nil
	}
	if websocketUpgrade {
		if method == "GET" && normalizedPath == NormalizedPathResponses {
			return nil
		}
		return UnsupportedEndpoint("websocket ingress is not supported on compatibility routes; use HTTP POST and SSE streaming semantics")
	}
	if method != "POST" {
		return UnsupportedEndpoint("compatibility family operations require HTTP POST")
	}
	return nil
}
