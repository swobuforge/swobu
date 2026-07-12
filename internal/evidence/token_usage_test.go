package evidence

import "testing"

func TestNewTokenUsageWithOptional_RejectsNegativeValues(t *testing.T) {
	negative := -1
	if _, err := NewTokenUsageWithOptional(&negative, nil, nil, nil); err == nil {
		t.Fatal("NewTokenUsageWithOptional should reject negative input tokens")
	}
	if _, err := NewTokenUsageWithOptional(nil, &negative, nil, nil); err == nil {
		t.Fatal("NewTokenUsageWithOptional should reject negative output tokens")
	}
	if _, err := NewTokenUsageWithOptional(nil, nil, &negative, nil); err == nil {
		t.Fatal("NewTokenUsageWithOptional should reject negative cache read tokens")
	}
	if _, err := NewTokenUsageWithOptional(nil, nil, nil, &negative); err == nil {
		t.Fatal("NewTokenUsageWithOptional should reject negative cache write tokens")
	}
	if _, err := NewTokenUsage(TokenUsageParams{ReasoningTokens: &negative}); err == nil {
		t.Fatal("NewTokenUsage should reject negative reasoning tokens")
	}
}

func TestTokenUsage_ReasoningTokensAndTotalKnownTokens(t *testing.T) {
	input := 21
	output := 8
	reasoning := 5
	cacheRead := 2
	cacheWrite := 1
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
	if got, ok := usage.ReasoningTokens(); !ok || got != 5 {
		t.Fatalf("ReasoningTokens = (%d,%v), want (5,true)", got, ok)
	}
	if got, ok := usage.TotalKnownTokens(); !ok || got != 29 {
		t.Fatalf("TotalKnownTokens = (%d,%v), want (29,true)", got, ok)
	}
}
