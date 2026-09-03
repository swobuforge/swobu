package core

import (
	"testing"
)

func TestExtractTokenUsage_MapsReasoningTokens(t *testing.T) {
	raw := []byte(`{
		"usage":{
			"input_tokens":10,
			"output_tokens":7,
			"total_tokens":19,
			"output_tokens_details":{"reasoning_tokens":4},
			"input_tokens_details":{"cached_tokens":3,"cache_write_tokens":1}
		}
	}`)
	usage := ExtractTokenUsage(raw, TokenUsagePathSpec{
		InputPaths: [][]string{
			{"usage", "input_tokens"},
		},
		OutputPaths: [][]string{
			{"usage", "output_tokens"},
		},
		ReasoningPaths: [][]string{
			{"usage", "output_tokens_details", "reasoning_tokens"},
		},
		CacheReadPaths: [][]string{
			{"usage", "input_tokens_details", "cached_tokens"},
		},
		CacheWritePaths: [][]string{
			{"usage", "input_tokens_details", "cache_write_tokens"},
		},
	})
	reasoning, ok := usage.ReasoningTokens()
	if !ok || reasoning != 4 {
		t.Fatalf("ReasoningTokens = (%d,%v), want (4,true)", reasoning, ok)
	}
	total, ok := usage.TotalKnownTokens()
	if !ok || total != 17 {
		t.Fatalf("TotalKnownTokens = (%d,%v), want derived (17,true)", total, ok)
	}
}

func TestExtractTokenUsage_ReturnsUnknownForMissingUsage(t *testing.T) {
	usage := ExtractTokenUsage([]byte(`{"id":"missing"}`), TokenUsagePathSpec{})
	if !usage.IsZero() {
		t.Fatalf("usage = %#v, want zero value", usage)
	}
	if _, ok := usage.ReasoningTokens(); ok {
		t.Fatal("reasoning tokens should be unknown")
	}
}

func TestExtractTokenUsage_PreservesCacheCounterPresence(t *testing.T) {
	spec := TokenUsagePathSpec{
		CacheReadPaths: [][]string{{"usage", "cached"}}, CacheWritePaths: [][]string{{"usage", "written"}},
	}
	tests := []struct {
		name    string
		raw     string
		value   int
		present bool
	}{
		{name: "absent", raw: `{"usage":{}}`},
		{name: "zero", raw: `{"usage":{"cached":0,"written":0}}`, present: true},
		{name: "positive", raw: `{"usage":{"cached":11,"written":7}}`, value: 11, present: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage := ExtractTokenUsage([]byte(test.raw), spec)
			read, readPresent := usage.CacheReadTokens()
			write, writePresent := usage.CacheWriteTokens()
			if readPresent != test.present || writePresent != test.present || read != test.value {
				t.Fatalf("read/write = (%d,%t)/(%d,%t)", read, readPresent, write, writePresent)
			}
			if test.name == "positive" && write != 7 {
				t.Fatalf("write = %d", write)
			}
		})
	}
}
