package canonical

import "github.com/swobuforge/swobu/internal/domain/protocolkind"

type ClientFamily = protocolkind.ProtocolKind

const (
	ClientFamilyChatCompletions ClientFamily = protocolkind.ChatCompletions
	ClientFamilyResponses       ClientFamily = protocolkind.Responses
	ClientFamilyMessages        ClientFamily = protocolkind.Messages
)

func InferClientFamily(method string, normalizedPath NormalizedPath, hasMessagesProtocolMarker bool) (ClientFamily, error) {
	switch {
	case method == "POST" && normalizedPath == NormalizedPathChatCompletions:
		return ClientFamilyChatCompletions, nil
	case method == "POST" && normalizedPath == NormalizedPathResponses:
		return ClientFamilyResponses, nil
	case method == "POST" && normalizedPath == NormalizedPathMessages && hasMessagesProtocolMarker:
		return ClientFamilyMessages, nil
	default:
		return "", UnsupportedEndpoint("unsupported or ambiguous client family")
	}
}

// ValidateClientTransport enforces protocol-route transport law before family decoding.
func ValidateClientTransport(method string, normalizedPath NormalizedPath, websocketUpgrade bool) error {
	if normalizedPath == NormalizedPathModels {
		return nil
	}
	if websocketUpgrade {
		if method == "GET" && normalizedPath == NormalizedPathResponses {
			return nil
		}
		return UnsupportedEndpoint("websocket client transport is not supported on protocol routes; use request-post with framed streaming delivery")
	}
	if method != "POST" {
		return UnsupportedEndpoint("protocol family operations require request-post method")
	}
	return nil
}
