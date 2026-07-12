package compat

import (
	"fmt"
	"strings"
)

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
