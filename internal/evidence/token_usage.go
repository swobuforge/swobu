package evidence

import "fmt"

// TokenUsage records provider-neutral token counters in runtime evidence.
// It remains optional because some providers or delivery paths do not report
// token accounting, including reasoning-token breakdowns, at terminal-event
// emission time.
type TokenUsage struct {
	inputTokens      int
	outputTokens     int
	reasoningTokens  int
	cacheReadTokens  int
	cacheWriteTokens int

	hasInputTokens      bool
	hasOutputTokens     bool
	hasReasoningTokens  bool
	hasCacheReadTokens  bool
	hasCacheWriteTokens bool
}

type TokenUsageParams struct {
	InputTokens      *int
	OutputTokens     *int
	ReasoningTokens  *int
	CacheReadTokens  *int
	CacheWriteTokens *int
}

func NewTokenUsage(params TokenUsageParams) (TokenUsage, error) {
	usage := TokenUsage{}
	if params.InputTokens != nil {
		if *params.InputTokens < 0 {
			return TokenUsage{}, fmt.Errorf("input tokens must not be negative")
		}
		usage.inputTokens = *params.InputTokens
		usage.hasInputTokens = true
	}
	if params.OutputTokens != nil {
		if *params.OutputTokens < 0 {
			return TokenUsage{}, fmt.Errorf("output tokens must not be negative")
		}
		usage.outputTokens = *params.OutputTokens
		usage.hasOutputTokens = true
	}
	if params.ReasoningTokens != nil {
		if *params.ReasoningTokens < 0 {
			return TokenUsage{}, fmt.Errorf("reasoning tokens must not be negative")
		}
		usage.reasoningTokens = *params.ReasoningTokens
		usage.hasReasoningTokens = true
	}
	if params.CacheReadTokens != nil {
		if *params.CacheReadTokens < 0 {
			return TokenUsage{}, fmt.Errorf("cache read tokens must not be negative")
		}
		usage.cacheReadTokens = *params.CacheReadTokens
		usage.hasCacheReadTokens = true
	}
	if params.CacheWriteTokens != nil {
		if *params.CacheWriteTokens < 0 {
			return TokenUsage{}, fmt.Errorf("cache write tokens must not be negative")
		}
		usage.cacheWriteTokens = *params.CacheWriteTokens
		usage.hasCacheWriteTokens = true
	}
	return usage, nil
}

func NewTokenUsageWithOptional(inputTokens *int, outputTokens *int, cacheReadTokens *int, cacheWriteTokens *int) (TokenUsage, error) {
	return NewTokenUsage(TokenUsageParams{
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		CacheReadTokens:  cacheReadTokens,
		CacheWriteTokens: cacheWriteTokens,
	})
}

func (u TokenUsage) InputTokens() (int, bool) {
	return u.inputTokens, u.hasInputTokens
}

func (u TokenUsage) OutputTokens() (int, bool) {
	return u.outputTokens, u.hasOutputTokens
}

func (u TokenUsage) ReasoningTokens() (int, bool) {
	return u.reasoningTokens, u.hasReasoningTokens
}

func (u TokenUsage) CacheReadTokens() (int, bool) {
	return u.cacheReadTokens, u.hasCacheReadTokens
}

func (u TokenUsage) CacheWriteTokens() (int, bool) {
	return u.cacheWriteTokens, u.hasCacheWriteTokens
}

func (u TokenUsage) TotalKnownTokens() (int, bool) {
	if !u.hasInputTokens || !u.hasOutputTokens {
		return 0, false
	}
	return u.inputTokens + u.outputTokens, true
}

func (u TokenUsage) IsZero() bool {
	return !u.hasInputTokens && !u.hasOutputTokens && !u.hasReasoningTokens && !u.hasCacheReadTokens && !u.hasCacheWriteTokens
}
