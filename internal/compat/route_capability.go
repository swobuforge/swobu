package compat

import "strings"

type routeKey struct {
	Provider string
	Protocol string
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
	featureSetCompletions = []Feature{
		GenerationMaxTokens,
		GenerationTemperature,
		GenerationTopP,
		GenerationStopSequences,
	}
)

var routeFeatureMatrix = buildRouteFeatureMatrix()

func buildRouteFeatureMatrix() map[routeKey][]Feature {
	out := map[routeKey][]Feature{}
	addRouteFeatures := func(provider string, protocol string, features []Feature) {
		key := routeKey{Provider: provider, Protocol: protocol}
		out[key] = append([]Feature(nil), features...)
	}
	addFamily := func(provider string) {
		addRouteFeatures(provider, "responses", featureSetResponses)
		addRouteFeatures(provider, "responses_stream", featureSetResponses)
		addRouteFeatures(provider, "chat_completions", featureSetChatCompletions)
		addRouteFeatures(provider, "chat_completions_stream", featureSetChatCompletions)
		addRouteFeatures(provider, "completions", featureSetCompletions)
		addRouteFeatures(provider, "completions_stream", featureSetCompletions)
	}
	addFamily("openai")
	addFamily("openrouter")
	addFamily("openai_compatible")
	addFamily("ollama")
	addFamily("bedrock")
	addFamily("azure")
	addRouteFeatures("chatgpt", "responses_stream", featureSetResponses)
	addRouteFeatures("anthropic", "messages", featureSetMessages)
	addRouteFeatures("anthropic", "messages_stream", featureSetMessages)
	addRouteFeatures("bedrock", "messages", featureSetMessages)
	addRouteFeatures("bedrock", "messages_stream", featureSetMessages)
	return out
}

// SupportsFeature reports the support level for one feature on one route.
func SupportsFeature(provider string, protocol string, model string, feature Feature) Support {
	features, ok := routeFeatures(routeKey{
		Provider: strings.TrimSpace(provider), // swobu:io-string source=boundary
		Protocol: strings.TrimSpace(protocol), // swobu:io-string source=boundary
		Model:    strings.TrimSpace(model),    // swobu:io-string source=boundary
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

func routeFeatures(key routeKey) ([]Feature, bool) {
	if features, ok := routeFeatureMatrix[key]; ok {
		return features, true
	}
	key.Model = ""
	features, ok := routeFeatureMatrix[key]
	return features, ok
}
