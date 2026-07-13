package completions

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestCompletionsUsageFromCanonical_PreservesReasoningTokens(t *testing.T) {
	input := 5
	output := 3
	reasoning := 2

	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens:     &input,
		OutputTokens:    &output,
		ReasoningTokens: &reasoning,
	})
	if err != nil {
		t.Fatalf("NewTokenUsage returned error: %v", err)
	}

	dto := completionsUsageFromCanonical(usage)
	if dto == nil {
		t.Fatal("completionsUsageFromCanonical returned nil, want usage DTO")
	}
	if dto.PromptTokens != input {
		t.Fatalf("PromptTokens = %d, want %d", dto.PromptTokens, input)
	}
	if dto.CompletionTokens != output {
		t.Fatalf("CompletionTokens = %d, want %d", dto.CompletionTokens, output)
	}
	if dto.TotalTokens != input+output {
		t.Fatalf("TotalTokens = %d, want %d", dto.TotalTokens, input+output)
	}
	if dto.CompletionDetails == nil {
		t.Fatal("CompletionDetails = nil, want reasoning details")
	}
	if dto.CompletionDetails.ReasoningTokens != reasoning {
		t.Fatalf("ReasoningTokens = %d, want %d", dto.CompletionDetails.ReasoningTokens, reasoning)
	}
}
