package canonical

import (
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type ClientFamily = protocolkind.ProtocolKind

const (
	ClientFamilyChatCompletions ClientFamily = protocolkind.ChatCompletions
	ClientFamilyResponses       ClientFamily = protocolkind.Responses
	ClientFamilyMessages        ClientFamily = protocolkind.Messages
	ClientFamilyGenerateContent ClientFamily = protocolkind.GenerateContent
)

// ClientOperation is the single parsed authority for an HTTP client model
// invocation. Body-owned fields remain unspecified for existing protocols.
type ClientOperation struct {
	Family         ClientFamily
	NormalizedPath NormalizedPath
	Model          Specified[string]
	Streaming      Specified[bool]
}

func ParseClientOperation(method, operationPath string, hasMessagesProtocolMarker, websocketUpgrade bool) (ClientOperation, error) {
	parsed, err := url.ParseRequestURI(operationPath)
	if err != nil {
		return ClientOperation{}, UnsupportedEndpoint("unsupported client operation")
	}
	operation, err := parseClientOperation(parsed, hasMessagesProtocolMarker)
	if err != nil {
		return operation, err
	}
	if websocketUpgrade {
		return operation, UnsupportedEndpoint("websocket client transport is not supported on protocol routes; use request-post with framed streaming delivery")
	}
	if method != "POST" {
		return operation, UnsupportedEndpoint("protocol family operations require request-post method")
	}
	return operation, nil
}

func parseClientOperation(parsed *url.URL, hasMessagesProtocolMarker bool) (ClientOperation, error) {
	switch parsed.Path {
	case "/chat/completions", "/v1/chat/completions":
		return ClientOperation{Family: ClientFamilyChatCompletions, NormalizedPath: NormalizedPathChatCompletions}, nil
	case "/responses", "/v1/responses":
		return ClientOperation{Family: ClientFamilyResponses, NormalizedPath: NormalizedPathResponses}, nil
	case "/messages", "/v1/messages":
		if !hasMessagesProtocolMarker {
			return ClientOperation{}, UnsupportedEndpoint("unsupported or ambiguous client family")
		}
		return ClientOperation{Family: ClientFamilyMessages, NormalizedPath: NormalizedPathMessages}, nil
	}
	return parseGenerateContentOperation(parsed)
}

func parseGenerateContentOperation(parsed *url.URL) (ClientOperation, error) {
	operation := ClientOperation{Family: ClientFamilyGenerateContent}
	path := parsed.EscapedPath()
	prefix := ""
	for _, candidate := range []string{"/v1beta/models/", "/v1/models/"} {
		if strings.HasPrefix(path, candidate) {
			prefix = candidate
			break
		}
	}
	if prefix == "" {
		return ClientOperation{}, UnsupportedEndpoint("unsupported client operation")
	}
	decodedPrefix := strings.TrimSuffix(prefix, "/") + "/"
	if !strings.HasPrefix(parsed.Path, decodedPrefix) {
		return operation, UnsupportedEndpoint("invalid GenerateContent model path")
	}
	decodedRemainder := strings.TrimPrefix(parsed.Path, decodedPrefix)
	remainder := strings.TrimPrefix(path, prefix)
	lower := strings.ToLower(remainder)
	if remainder == "" || strings.ContainsAny(remainder, "/\\") || strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(decodedRemainder, "..") {
		return operation, UnsupportedEndpoint("invalid GenerateContent model path")
	}
	model, action, ok := strings.Cut(decodedRemainder, ":")
	if !ok || model == "" || action == "" || strings.Contains(action, ":") {
		return operation, UnsupportedEndpoint("invalid GenerateContent operation")
	}
	streaming := false
	normalized := NormalizedPathGenerateContent
	switch action {
	case "generateContent":
	case "streamGenerateContent":
		streaming = true
		normalized = NormalizedPathStreamGenerateContent
	default:
		return operation, UnsupportedEndpoint("unsupported GenerateContent operation")
	}
	query := parsed.Query()
	for key := range query {
		if key != "alt" && key != "key" {
			return operation, UnsupportedEndpoint("unsupported GenerateContent query parameter")
		}
	}
	for _, alt := range query["alt"] {
		if alt != "sse" {
			return operation, UnsupportedEndpoint("GenerateContent alt must be sse when specified")
		}
	}
	operation.NormalizedPath = normalized
	operation.Model = Specify(model)
	operation.Streaming = Specify(streaming)
	return operation, nil
}
