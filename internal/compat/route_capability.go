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
	providerFamilyProtocols = []string{
		"responses",
		"responses_stream",
		"chat_completions",
		"chat_completions_stream",
		"completions",
		"completions_stream",
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
		for _, protocol := range providerFamilyProtocols {
			switch protocol {
			case "responses", "responses_stream":
				addRouteFeatures(provider, protocol, featureSetResponses)
			case "chat_completions", "chat_completions_stream":
				addRouteFeatures(provider, protocol, featureSetChatCompletions)
			case "completions", "completions_stream":
				addRouteFeatures(provider, protocol, featureSetCompletions)
			}
		}
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

// CapabilitiesForRoute returns the support facts for one route/protocol/model
// tuple. Unknown routes return nil.
func CapabilitiesForRoute(provider string, protocol string, model string) []Capability {
	provider = strings.TrimSpace(provider)
	protocol = strings.TrimSpace(protocol)
	model = strings.TrimSpace(model)
	features, ok := routeFeatures(routeKey{
		Provider: provider,
		Protocol: protocol,
		Model:    model,
	})
	if !ok {
		return nil
	}
	caps := make([]Capability, 0, len(features))
	for _, feature := range features {
		caps = append(caps, Capability{
			Feature:  feature,
			Support:  Supported,
			Provider: provider,
			Protocol: protocol,
			Model:    model,
		})
	}
	return caps
}

// SupportsFeature reports the support level for one feature on one route.
func SupportsFeature(provider string, protocol string, model string, feature Feature) Support {
	features, ok := routeFeatures(routeKey{
		Provider: strings.TrimSpace(provider),
		Protocol: strings.TrimSpace(protocol),
		Model:    strings.TrimSpace(model),
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
