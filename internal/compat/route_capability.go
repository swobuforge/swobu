package compat

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type routeKey struct {
	Provider string
	Protocol protocolkind.ProtocolKind
	Model    string
}

var (
	featureSetResponses = []Feature{
		ToolDeclaration,
		RequestToolChoice,
		RequestParallelTools,
		RequestStructuredOutput,
		GenerationMaxTokens,
		GenerationTemperature,
		GenerationTopP,
		UsageReasoningTokens,
	}
	featureSetChatCompletions = []Feature{
		ToolDeclaration,
		RequestToolChoice,
		RequestParallelTools,
		RequestStructuredOutput,
		GenerationMaxTokens,
		GenerationTemperature,
		GenerationTopP,
		GenerationStopSequences,
		UsageReasoningTokens,
	}
	featureSetMessages = []Feature{
		ToolDeclaration,
		RequestToolChoice,
		RequestParallelTools,
		GenerationMaxTokens,
		GenerationTemperature,
		GenerationTopP,
		GenerationStopSequences,
	}
)

var routeFeatureMatrix = buildRouteFeatureMatrix()

func buildRouteFeatureMatrix() map[routeKey][]Feature {
	out := map[routeKey][]Feature{}
	addRouteFeatures := func(provider string, protocol protocolkind.ProtocolKind, features []Feature) {
		key := routeKey{Provider: provider, Protocol: protocol}
		out[key] = append([]Feature(nil), features...)
	}
	addFamily := func(provider string) {
		addRouteFeatures(provider, protocolkind.Responses, featureSetResponses)
		addRouteFeatures(provider, protocolkind.ChatCompletions, featureSetChatCompletions)
	}
	addFamily("openai")
	addFamily("openrouter")
	addFamily("custom")
	addFamily("bedrock")
	addFamily("azure")
	addRouteFeatures("ollama", protocolkind.ChatCompletions, featureSetChatCompletions)
	addRouteFeatures("chatgpt", protocolkind.Responses, featureSetResponses)
	addRouteFeatures("anthropic", protocolkind.Messages, featureSetMessages)
	addRouteFeatures("bedrock", protocolkind.Messages, featureSetMessages)
	addRouteFeatures("custom", protocolkind.Messages, featureSetMessages)
	return out
}

// SupportsFeature reports the support level for one feature on one route.
func SupportsFeature(provider string, protocol protocolkind.ProtocolKind, model string, feature Feature) Support {
	provider = strings.TrimSpace(provider) // swobu:io-string source=boundary
	model = strings.TrimSpace(model)       // swobu:io-string source=boundary
	if provider == "azure" && isAzureClaudeDeployment(model) {
		switch protocol {
		case protocolkind.Messages:
			for _, supported := range featureSetMessages {
				if supported == feature {
					return Supported
				}
			}
		}
	}
	features, ok := routeFeatures(routeKey{
		Provider: provider,
		Protocol: protocol,
		Model:    model,
	})
	if !ok {
		return Unknown
	}
	for _, supported := range features {
		if supported == feature {
			return Supported
		}
	}
	return Unsupported
}

func isAzureClaudeDeployment(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(normalized, "claude-")
}

func routeFeatures(key routeKey) ([]Feature, bool) {
	if features, ok := routeFeatureMatrix[key]; ok {
		return features, true
	}
	key.Model = ""
	features, ok := routeFeatureMatrix[key]
	return features, ok
}
