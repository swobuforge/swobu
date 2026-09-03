package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResponsesUsageProjectionDoesNotFabricateUnknownBaseCounters(t *testing.T) {
	value := 7
	tests := []struct {
		name   string
		params canonical.TokenUsageParams
	}{
		{name: "input only", params: canonical.TokenUsageParams{InputTokens: &value}},
		{name: "output only", params: canonical.TokenUsageParams{OutputTokens: &value}},
		{name: "detail only", params: canonical.TokenUsageParams{ReasoningTokens: &value}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage, err := canonical.NewTokenUsage(test.params)
			if err != nil {
				t.Fatal(err)
			}
			if projected := responsesUsageFromCanonical(usage); projected != nil {
				t.Fatalf("unrepresentable usage projected fabricated bases: %#v", projected)
			}
		})
	}
}

func TestResponsesUsageProjectionOmitsCacheDetailsWhenCacheReadIsUnknown(t *testing.T) {
	input, output, cacheWrite := 20, 4, 17
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens: &input, OutputTokens: &output, CacheWriteTokens: &cacheWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	projected := responsesUsageFromCanonical(usage)
	if projected == nil {
		t.Fatal("representable base usage was omitted")
	}
	if projected.InputDetails != nil {
		t.Fatalf("cache-write-only detail fabricated cached_tokens: %#v", projected.InputDetails)
	}
}

func TestResponsesUsageProjectionPreservesKnownZeroCacheWrite(t *testing.T) {
	input, output, cacheRead, cacheWrite := 20, 4, 5, 0
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens: &input, OutputTokens: &output,
		CacheReadTokens: &cacheRead, CacheWriteTokens: &cacheWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	projected := responsesUsageFromCanonical(usage)
	if projected == nil || projected.InputDetails == nil || projected.InputDetails.CacheWriteTokens == nil || *projected.InputDetails.CacheWriteTokens != 0 {
		t.Fatalf("known zero cache write was erased: %#v", projected)
	}
}
