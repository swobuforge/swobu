package canonical

import "testing"

func TestGenerationControls_CloneDeepCopiesStopSequences(t *testing.T) {
	maxTokens := 256
	temperature := 0.4
	topP := 0.9
	controls, err := NewGenerationControls(GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
		StopSequences:   []string{"END", "DONE"},
		Temperature:     &temperature,
		TopP:            &topP,
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}

	cloned := controls.Clone()
	cloned.Limits.StopSequences[0] = "mutated"

	if got, ok := controls.Limits.MaxOutputTokens.Value(); !ok || got != 256 {
		t.Fatalf("max_output_tokens = (%d, %v), want (256, true)", got, ok)
	}
	if got := controls.Limits.StopSequences; len(got) != 2 || got[0] != "END" || got[1] != "DONE" {
		t.Fatalf("stop sequences = %#v, want [END DONE]", got)
	}
	if got, ok := controls.Sampling.Temperature.Value(); !ok || got != 0.4 {
		t.Fatalf("temperature = (%v, %v), want (0.4, true)", got, ok)
	}
	if got, ok := controls.Sampling.TopP.Value(); !ok || got != 0.9 {
		t.Fatalf("top_p = (%v, %v), want (0.9, true)", got, ok)
	}
	if got := cloned.Limits.StopSequences; len(got) != 2 || got[0] != "mutated" {
		t.Fatalf("cloned stop sequences did not mutate as expected: %#v", got)
	}
}

func TestGenerationControls_RejectsInvalidValues(t *testing.T) {
	negative := -1
	zero := 0
	tooHigh := 1.1
	negativeTemp := -0.1

	tests := []struct {
		name   string
		params GenerationControlsParams
	}{
		{
			name:   "max output tokens",
			params: GenerationControlsParams{MaxOutputTokens: &zero},
		},
		{
			name:   "stop sequence",
			params: GenerationControlsParams{StopSequences: []string{" "}},
		},
		{
			name:   "empty stop sequence list",
			params: GenerationControlsParams{StopSequences: []string{}},
		},
		{
			name:   "temperature",
			params: GenerationControlsParams{Temperature: &negativeTemp},
		},
		{
			name:   "top p",
			params: GenerationControlsParams{TopP: &tooHigh},
		},
		{
			name:   "max output negative",
			params: GenerationControlsParams{MaxOutputTokens: &negative},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewGenerationControls(tc.params); err == nil {
				t.Fatal("NewGenerationControls returned nil error, want validation failure")
			}
		})
	}
}

func TestGenerationControls_MetadataRoundTrips(t *testing.T) {
	maxTokens := 512
	temperature := 0.7
	topP := 0.85
	controls, err := NewGenerationControls(GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
		StopSequences:   []string{"END"},
		Temperature:     &temperature,
		TopP:            &topP,
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}

	raw, err := encodeGenerationControlsMetadata(controls)
	if err != nil {
		t.Fatalf("encodeGenerationControlsMetadata returned error: %v", err)
	}
	got, err := decodeGenerationControlsMetadata(raw)
	if err != nil {
		t.Fatalf("decodeGenerationControlsMetadata returned error: %v", err)
	}
	if got.IsZero() {
		t.Fatal("decoded controls are zero, want populated controls")
	}
	if max, ok := got.Limits.MaxOutputTokens.Value(); !ok || max != 512 {
		t.Fatalf("decoded max_output_tokens = (%d, %v), want (512, true)", max, ok)
	}
	if got.Limits.StopSequences[0] != "END" {
		t.Fatalf("decoded stop sequence = %q, want %q", got.Limits.StopSequences[0], "END")
	}
	if temperature, ok := got.Sampling.Temperature.Value(); !ok || temperature != 0.7 {
		t.Fatalf("decoded temperature = (%v, %v), want (0.7, true)", temperature, ok)
	}
	if topP, ok := got.Sampling.TopP.Value(); !ok || topP != 0.85 {
		t.Fatalf("decoded top_p = (%v, %v), want (0.85, true)", topP, ok)
	}
}
