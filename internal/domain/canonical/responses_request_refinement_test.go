package canonical

import "testing"

func TestResponsesRequestRefinementPreservesStoreOccurrenceAndEligibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		field         Specified[bool]
		wantValue     bool
		wantSpecified bool
		wantEligible  bool
	}{
		{name: "omitted", field: Unspecified[bool](), wantEligible: true},
		{name: "false", field: Specify(false), wantSpecified: true},
		{name: "true", field: Specify(true), wantValue: true, wantSpecified: true, wantEligible: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			refinement := NewResponsesRequestRefinement(test.field)
			clone := refinement.Clone()
			gotValue, gotSpecified := clone.Store()
			if gotValue != test.wantValue || gotSpecified != test.wantSpecified {
				t.Fatalf("cloned store = (%t,%t), want (%t,%t)", gotValue, gotSpecified, test.wantValue, test.wantSpecified)
			}
			if got := clone.PersistenceEligible(); got != test.wantEligible {
				t.Fatalf("PersistenceEligible() = %t, want %t", got, test.wantEligible)
			}
		})
	}
}
