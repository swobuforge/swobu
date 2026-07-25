package canonical

type NormalizedPath string

const (
	NormalizedPathChatCompletions NormalizedPath = "/chat/completions"
	NormalizedPathResponses       NormalizedPath = "/responses"
	NormalizedPathMessages        NormalizedPath = "/messages"
	NormalizedPathModels          NormalizedPath = "/models"
)

// ValidNormalizedPath reports whether p is one of the canonical normalized
// request paths. It is a strict membership test (unlike NormalizePath, which also
// accepts /v1-prefixed raw inputs), so the terminal-event constructor can reject a
// typed-but-non-canonical value at the source rather than carry it to the report.
func ValidNormalizedPath(p NormalizedPath) bool {
	switch p {
	case NormalizedPathChatCompletions, NormalizedPathResponses, NormalizedPathMessages, NormalizedPathModels:
		return true
	}
	return false
}

func NormalizePath(raw string) (NormalizedPath, error) {
	path := raw // swobu:io-string source=http-path
	switch path {
	case "/chat/completions", "/v1/chat/completions":
		return NormalizedPathChatCompletions, nil
	case "/responses", "/v1/responses":
		return NormalizedPathResponses, nil
	case "/messages", "/v1/messages":
		return NormalizedPathMessages, nil
	case "/models", "/v1/models":
		return NormalizedPathModels, nil
	default:
		return "", UnsupportedEndpoint("unsupported normalized path")
	}
}
