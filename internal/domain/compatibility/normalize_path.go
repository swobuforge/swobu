package compatibility

type NormalizedPath string

const (
	NormalizedPathChatCompletions NormalizedPath = "/chat/completions"
	NormalizedPathResponses       NormalizedPath = "/responses"
	NormalizedPathCompletions     NormalizedPath = "/completions"
	NormalizedPathMessages        NormalizedPath = "/messages"
	NormalizedPathModels          NormalizedPath = "/models"
)

func NormalizePath(raw string) (NormalizedPath, error) {
	switch raw {
	case "/chat/completions", "/v1/chat/completions":
		return NormalizedPathChatCompletions, nil
	case "/responses", "/v1/responses":
		return NormalizedPathResponses, nil
	case "/completions", "/v1/completions":
		return NormalizedPathCompletions, nil
	case "/messages", "/v1/messages":
		return NormalizedPathMessages, nil
	case "/models", "/v1/models":
		return NormalizedPathModels, nil
	default:
		return "", UnsupportedEndpoint("unsupported normalized path")
	}
}
