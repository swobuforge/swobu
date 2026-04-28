package runtimeevidence

import "fmt"

// TokenUsage records provider-neutral token counters in runtime evidence.
// It remains optional because some providers or delivery paths do not report
// token accounting at terminal-event emission time.
type TokenUsage struct {
	inputTokens      int
	outputTokens     int
	cacheReadTokens  int
	cacheWriteTokens int

	hasInputTokens      bool
	hasOutputTokens     bool
	hasCacheReadTokens  bool
	hasCacheWriteTokens bool
}

func NewUnknownTokenUsage() TokenUsage {
	return TokenUsage{}
}

func NewTokenUsageWithOptional(inputTokens *int, outputTokens *int, cacheReadTokens *int, cacheWriteTokens *int) (TokenUsage, error) {
	usage := TokenUsage{}
	if inputTokens != nil {
		if *inputTokens < 0 {
			return TokenUsage{}, fmt.Errorf("input tokens must not be negative")
		}
		usage.inputTokens = *inputTokens
		usage.hasInputTokens = true
	}
	if outputTokens != nil {
		if *outputTokens < 0 {
			return TokenUsage{}, fmt.Errorf("output tokens must not be negative")
		}
		usage.outputTokens = *outputTokens
		usage.hasOutputTokens = true
	}
	if cacheReadTokens != nil {
		if *cacheReadTokens < 0 {
			return TokenUsage{}, fmt.Errorf("cache read tokens must not be negative")
		}
		usage.cacheReadTokens = *cacheReadTokens
		usage.hasCacheReadTokens = true
	}
	if cacheWriteTokens != nil {
		if *cacheWriteTokens < 0 {
			return TokenUsage{}, fmt.Errorf("cache write tokens must not be negative")
		}
		usage.cacheWriteTokens = *cacheWriteTokens
		usage.hasCacheWriteTokens = true
	}
	return usage, nil
}

func (u TokenUsage) InputTokens() (int, bool) {
	return u.inputTokens, u.hasInputTokens
}

func (u TokenUsage) OutputTokens() (int, bool) {
	return u.outputTokens, u.hasOutputTokens
}

func (u TokenUsage) CacheReadTokens() (int, bool) {
	return u.cacheReadTokens, u.hasCacheReadTokens
}

func (u TokenUsage) CacheWriteTokens() (int, bool) {
	return u.cacheWriteTokens, u.hasCacheWriteTokens
}

func (u TokenUsage) IsZero() bool {
	return !u.hasInputTokens && !u.hasOutputTokens && !u.hasCacheReadTokens && !u.hasCacheWriteTokens
}
