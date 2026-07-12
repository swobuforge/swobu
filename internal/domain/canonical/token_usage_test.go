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
