package compat

import (
	"fmt"
	"strings"
)

// Capability describes one route-scoped support fact for one feature.
type Capability struct {
	Feature    Feature           `json:"feature"`
	Support    Support           `json:"support"`
	Provider   string            `json:"provider,omitempty"`
	Protocol   string            `json:"protocol,omitempty"`
	Model      string            `json:"model,omitempty"`
	Qualifiers map[string]string `json:"qualifiers,omitempty"`
}

var knownFeatureSet = map[Feature]struct{}{
	RequestInputShape:        {},
	RequestModel:             {},
	RequestRole:              {},
	RequestToolChoice:        {},
	RequestParallelTools:     {},
	RequestStructuredOutput:  {},
	RequestContinuation:      {},
	RequestConversation:      {},
	MessageRole:              {},
	MessageAuthor:            {},
	ContentPartKind:          {},
	ContentText:              {},
	ContentImage:             {},
	ContentAudio:             {},
	ContentFile:              {},
	ContentRefusal:           {},
	ToolDeclaration:          {},
	ToolKind:                 {},
	ToolName:                 {},
	ToolNameNamespace:        {},
	ToolDescription:          {},
	ToolSchema:               {},
	ToolSchemaStrict:         {},
	ToolCallID:               {},
	ToolCallKind:             {},
	ToolCallArguments:        {},
	ToolResultID:             {},
	ToolResultBody:           {},
	GenerationMaxTokens:      {},
	GenerationTemperature:    {},
	GenerationTopP:           {},
	GenerationStopSequences:  {},
	GenerationSeed:           {},
	GenerationMultiplicity:   {},
	OutputFormat:             {},
	OutputJSONSchema:         {},
	OutputTextFormat:         {},
	OutputItemKind:           {},
	ResponseReasoning:        {},
	ResponseFinish:           {},
	ResponseError:            {},
	UsageInputTokens:         {},
	UsageOutputTokens:        {},
	UsageReasoningTokens:     {},
	UsageCacheReadTokens:     {},
	UsageCacheWriteTokens:    {},
	DeliveryStreaming:        {},
	DeliveryServerSentEvents: {},
	DeliveryWebSocket:        {},
	DeliveryIncremental:      {},
	DeliveryTerminalEvent:    {},
	WireJSONMode:             {},
	WireRawPayload:           {},
	WireNativePayload:        {},
	StateTurnSnapshot:        {},
	ErrorShape:               {},
	ErrorClass:               {},
}

// ValidateFeature reports whether one feature identifier follows the canonical
// feature inventory.
func ValidateFeature(feature Feature) error {
	normalized := strings.TrimSpace(string(feature))
	if normalized == "" {
		return fmt.Errorf("feature is required")
	}
	if _, ok := knownFeatureSet[Feature(normalized)]; !ok {
		return fmt.Errorf("feature %q is invalid", normalized)
	}
	return nil
}

// ValidateSubject reports whether one decision subject matches the canonical
// subject grammar. Empty subjects are permitted.
func ValidateSubject(subject Subject) error {
	normalized := strings.TrimSpace(string(subject))
	if normalized == "" {
		return nil
	}
	prefix, locator, ok := strings.Cut(normalized, ":")
	if !ok || prefix == "" || locator == "" {
		return fmt.Errorf("subject %q is invalid", normalized)
	}
	switch prefix {
	case "wire":
		if !strings.HasPrefix(locator, "/") {
			return fmt.Errorf("subject %q is invalid", normalized)
		}
	case "canonical", "state", "event", "route", "provider":
		// These locators may use semantic path syntax or route path syntax,
		// but they must still stay single-token and non-empty.
	default:
		return fmt.Errorf("subject %q is invalid", normalized)
	}
	if strings.Contains(locator, " ") {
		return fmt.Errorf("subject %q is invalid", normalized)
	}
	return nil
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
