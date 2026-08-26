package reasoningprojection

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEffortFromReferenceReasoningBudgetAnchorsAndBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tokens int
		want   canonical.InferenceEffort
	}{
		{1_024, canonical.InferenceEffortLow},
		{8_192, canonical.InferenceEffortMedium},
		{24_576, canonical.InferenceEffortHigh},
		{2_896, canonical.InferenceEffortLow},
		{2_897, canonical.InferenceEffortMedium},
		{14_188, canonical.InferenceEffortMedium},
		{14_189, canonical.InferenceEffortHigh},
	}
	for _, test := range tests {
		if got := EffortFromReferenceReasoningBudget(test.tokens); got != test.want {
			t.Errorf("EffortFromReferenceReasoningBudget(%d) = %q, want %q", test.tokens, got, test.want)
		}
	}
}

func TestEffortFromReferenceReasoningBudgetIsMonotonic(t *testing.T) {
	t.Parallel()

	rank := map[canonical.InferenceEffort]int{
		canonical.InferenceEffortLow:    1,
		canonical.InferenceEffortMedium: 2,
		canonical.InferenceEffortHigh:   3,
	}
	previous := 0
	for tokens := 1; tokens <= 100_000; tokens++ {
		current := rank[EffortFromReferenceReasoningBudget(tokens)]
		if current < previous {
			t.Fatalf("projection rank decreased at budget %d: %d < %d", tokens, current, previous)
		}
		previous = current
	}
}

func TestEffortFromReferenceReasoningBudgetRejectsInvalidBudget(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		tokens int
	}{
		{name: "zero", tokens: 0},
		{name: "negative", tokens: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("reference reasoning budget %d did not panic", test.tokens)
				}
			}()
			EffortFromReferenceReasoningBudget(test.tokens)
		})
	}
}

func TestProjectOrdinalReasoningLaw(t *testing.T) {
	t.Parallel()

	automatic := canonical.NewAutomaticReasoningCompute()
	disabled := canonical.NewDisabledReasoningCompute()
	budget, err := canonical.NewBudgetReasoningCompute(10_000)
	if err != nil {
		t.Fatal(err)
	}
	low := canonical.Specify(canonical.InferenceEffortLow)

	tests := []struct {
		name        string
		compute     *canonical.ReasoningCompute
		effort      canonical.Specified[canonical.InferenceEffort]
		wantKind    OrdinalKind
		wantEffort  canonical.InferenceEffort
		wantChanges []compat.Change
	}{
		{name: "unspecified"},
		{name: "effort only", effort: low, wantKind: OrdinalEffort, wantEffort: canonical.InferenceEffortLow},
		{name: "disabled", compute: &disabled, wantKind: OrdinalDisabled},
		{
			name: "disabled dominates effort", compute: &disabled, effort: low,
			wantKind:    OrdinalDisabled,
			wantChanges: []compat.Change{compat.NewOmission(canonical.RequestControlsEffort, canonical.Occurrence{})},
		},
		{name: "automatic remains automatic", compute: &automatic, wantKind: OrdinalAutomatic},
		{name: "automatic preserves effort", compute: &automatic, effort: low, wantKind: OrdinalEffort, wantEffort: canonical.InferenceEffortLow},
		{
			name: "budget derives effort", compute: &budget,
			wantKind: OrdinalEffort, wantEffort: canonical.InferenceEffortMedium,
			wantChanges: []compat.Change{compat.NewApproximation(canonical.RequestReasoning, canonical.RequestControlsEffort, canonical.Occurrence{})},
		},
		{
			name: "effort dominates budget", compute: &budget, effort: low,
			wantKind: OrdinalEffort, wantEffort: canonical.InferenceEffortLow,
			wantChanges: []compat.Change{compat.NewApproximation(canonical.RequestReasoning, canonical.RequestControlsEffort, canonical.Occurrence{})},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reasoning := canonical.ReasoningControls{}
			if test.compute != nil {
				var err error
				reasoning, err = canonical.NewReasoningControls(canonical.ReasoningControlsParams{
					Compute: canonical.Specify(*test.compute),
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			projection := ProjectOrdinalReasoning(reasoning, test.effort)
			if projection.Kind != test.wantKind || projection.Effort != test.wantEffort {
				t.Fatalf("projection = %#v, want kind=%d effort=%q", projection, test.wantKind, test.wantEffort)
			}
			if len(projection.Changes) != len(test.wantChanges) {
				t.Fatalf("changes = %#v, want %#v", projection.Changes, test.wantChanges)
			}
			for i := range projection.Changes {
				if projection.Changes[i] != test.wantChanges[i] {
					t.Fatalf("change %d = %#v, want %#v", i, projection.Changes[i], test.wantChanges[i])
				}
			}
		})
	}
}

func TestProjectOrdinalReasoningPreservesEveryExplicitEffort(t *testing.T) {
	t.Parallel()

	budget, err := canonical.NewBudgetReasoningCompute(24_576)
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(budget),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, effort := range []canonical.InferenceEffort{
		canonical.InferenceEffortMinimal,
		canonical.InferenceEffortLow,
		canonical.InferenceEffortMedium,
		canonical.InferenceEffortHigh,
		canonical.InferenceEffortXHigh,
		canonical.InferenceEffortMax,
	} {
		t.Run(string(effort), func(t *testing.T) {
			projection := ProjectOrdinalReasoning(reasoning, canonical.Specify(effort))
			if projection.Kind != OrdinalEffort || projection.Effort != effort {
				t.Fatalf("projection = %#v, want exact explicit effort %q", projection, effort)
			}
			if len(projection.Changes) != 1 ||
				projection.Changes[0].Capability != canonical.RequestReasoning ||
				projection.Changes[0].Kind != compat.Approximation ||
				projection.Changes[0].Preserved != canonical.RequestControlsEffort {
				t.Fatalf("changes = %#v, want one compute approximation through explicit effort", projection.Changes)
			}
		})
	}
}
