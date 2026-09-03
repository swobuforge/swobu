package canonical

import "testing"

func TestTokenUsage_ReasoningTokensAndTotalKnownTokens(t *testing.T) {
	input := 12
	output := 7
	reasoning := 4
	cacheRead := 3
	cacheWrite := 2
	usage, err := NewTokenUsage(TokenUsageParams{
		InputTokens:      &input,
		OutputTokens:     &output,
		ReasoningTokens:  &reasoning,
		CacheReadTokens:  &cacheRead,
		CacheWriteTokens: &cacheWrite,
	})
	if err != nil {
		t.Fatalf("NewTokenUsage returned error: %v", err)
	}
	if got, ok := usage.ReasoningTokens(); !ok || got != 4 {
		t.Fatalf("ReasoningTokens = (%d,%v), want (4,true)", got, ok)
	}
	if got, ok := usage.TotalKnownTokens(); !ok || got != 19 {
		t.Fatalf("TotalKnownTokens = (%d,%v), want (19,true)", got, ok)
	}
	if usage.IsZero() {
		t.Fatal("usage should not be zero when reasoning tokens are present")
	}
}

func TestTokenUsage_RejectsNegativeReasoningTokens(t *testing.T) {
	negative := -1
	if _, err := NewTokenUsage(TokenUsageParams{ReasoningTokens: &negative}); err == nil {
		t.Fatal("NewTokenUsage should reject negative reasoning tokens")
	}
}

func TestTokenUsage_TotalRequiresInputAndOutput(t *testing.T) {
	value := 12
	for name, usage := range map[string]TokenUsage{
		"input only":  mustTokenUsage(t, TokenUsageParams{InputTokens: &value}),
		"output only": mustTokenUsage(t, TokenUsageParams{OutputTokens: &value}),
	} {
		t.Run(name, func(t *testing.T) {
			if total, known := usage.TotalKnownTokens(); known || total != 0 {
				t.Fatalf("TotalKnownTokens = (%d,%v), want (0,false)", total, known)
			}
		})
	}
}

func TestSumTokenUsageAccumulatesRoundsWithoutInventingUnknownFields(t *testing.T) {
	inputOne, outputOne := 10, 2
	inputTwo, outputTwo := 7, 3
	first, _ := NewTokenUsage(TokenUsageParams{InputTokens: &inputOne, OutputTokens: &outputOne})
	second, _ := NewTokenUsage(TokenUsageParams{InputTokens: &inputTwo, OutputTokens: &outputTwo})
	total := SumTokenUsage(first, second)
	if input, ok := total.InputTokens(); !ok || input != 17 {
		t.Fatalf("input total = %d, %v", input, ok)
	}
	if output, ok := total.OutputTokens(); !ok || output != 5 {
		t.Fatalf("output total = %d, %v", output, ok)
	}
	if _, ok := total.ReasoningTokens(); ok {
		t.Fatal("unknown reasoning usage became a known total")
	}
}

func mustTokenUsage(t *testing.T, params TokenUsageParams) TokenUsage {
	t.Helper()
	usage, err := NewTokenUsage(params)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}
