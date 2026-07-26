package canonical

import "fmt"

// TokenUsage captures provider-neutral token accounting for one successful output.
// It allows adapter edges to expose usage, cache truth, and reasoning-token
// breakdowns without leaking provider-dialect field names into canonical
// semantics.
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

func NewUnknownTokenUsage() TokenUsage {
	return TokenUsage{}
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

// SumTokenUsage combines complete per-round accounting. A field remains
// unknown when any round omitted it, preventing a partial sum from being
// reported as the exchange total.
func SumTokenUsage(rounds ...TokenUsage) TokenUsage {
	if len(rounds) == 0 {
		return NewUnknownTokenUsage()
	}
	type value struct {
		total int
		known bool
	}
	input := value{known: true}
	output := value{known: true}
	reasoning := value{known: true}
	cacheRead := value{known: true}
	cacheWrite := value{known: true}
	for _, round := range rounds {
		add := func(target *value, amount int, known bool) {
			target.known = target.known && known
			if known {
				target.total += amount
			}
		}
		amount, known := round.InputTokens()
		add(&input, amount, known)
		amount, known = round.OutputTokens()
		add(&output, amount, known)
		amount, known = round.ReasoningTokens()
		add(&reasoning, amount, known)
		amount, known = round.CacheReadTokens()
		add(&cacheRead, amount, known)
		amount, known = round.CacheWriteTokens()
		add(&cacheWrite, amount, known)
	}
	pointer := func(value value) *int {
		if !value.known {
			return nil
		}
		total := value.total
		return &total
	}
	usage, _ := NewTokenUsage(TokenUsageParams{
		InputTokens: pointer(input), OutputTokens: pointer(output),
		ReasoningTokens: pointer(reasoning), CacheReadTokens: pointer(cacheRead),
		CacheWriteTokens: pointer(cacheWrite),
	})
	return usage
}
