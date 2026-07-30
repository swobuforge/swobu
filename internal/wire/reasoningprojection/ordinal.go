package reasoningprojection

import (
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const (
	referenceLow    = 1_024
	referenceMedium = 8_192
	referenceHigh   = 24_576

	// The nearest-anchor boundary under logarithmic distance is the geometric
	// mean. Integer budgets at the floored mean remain in the lower band.
	// floor(sqrt(referenceLow * referenceMedium))
	lowMediumBoundary = 2_896
	// floor(sqrt(referenceMedium * referenceHigh))
	mediumHighBoundary = 14_188
)

// EffortFromReferenceReasoningBudget maps a positive reasoning budget to the
// nearest low, medium, or high reference anchor by logarithmic distance.
//
// Canonical construction guarantees a positive budget. A non-positive input
// is therefore an internal invariant violation, not a value to approximate.
func EffortFromReferenceReasoningBudget(tokens int) canonical.InferenceEffort {
	if tokens <= 0 {
		panic("reference reasoning budget must be positive")
	}
	switch {
	case tokens <= lowMediumBoundary:
		return canonical.InferenceEffortLow
	case tokens <= mediumHighBoundary:
		return canonical.InferenceEffortMedium
	default:
		return canonical.InferenceEffortHigh
	}
}

// ProjectOrdinalReasoning projects independent canonical compute and effort
// controls into one ordinal reasoning field. Exact effort dominates derived
// effort, while disabled compute is a hard-off constraint.
func ProjectOrdinalReasoning(
	reasoning canonical.ReasoningControls,
	effort canonical.Specified[canonical.InferenceEffort],
) (value string, present bool, changes []compat.Change) {
	compute, computeSpecified := reasoning.ComputeField().Get()
	explicitEffort, effortSpecified := effort.Get()

	if computeSpecified && compute.Kind() == canonical.ReasoningDisabled {
		if effortSpecified {
			changes = compat.AppendUnique(changes, compat.NewOmission(
				canonical.RequestControlsEffort,
				canonical.Occurrence{},
			))
		}
		return "none", true, changes
	}

	if effortSpecified {
		if computeSpecified && compute.Kind() == canonical.ReasoningBudget {
			changes = compat.AppendUnique(changes, reasoningApproximation())
		}
		return string(explicitEffort), true, changes
	}

	if !computeSpecified {
		return "", false, nil
	}

	switch compute.Kind() {
	case canonical.ReasoningAutomatic:
		changes = compat.AppendUnique(changes, reasoningApproximation())
		return string(canonical.InferenceEffortMedium), true, changes
	case canonical.ReasoningBudget:
		tokens, _ := compute.Tokens()
		changes = compat.AppendUnique(changes, reasoningApproximation())
		return string(EffortFromReferenceReasoningBudget(tokens)), true, changes
	default:
		return "", false, nil
	}
}

func reasoningApproximation() compat.Change {
	return compat.NewApproximation(
		canonical.RequestReasoning,
		canonical.RequestControlsEffort,
		canonical.Occurrence{},
	)
}
