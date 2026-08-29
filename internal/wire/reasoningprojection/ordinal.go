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

type OrdinalKind uint8

const (
	OrdinalUnspecified OrdinalKind = iota
	OrdinalAutomatic
	OrdinalDisabled
	OrdinalEffort
)

type OrdinalProjection struct {
	Kind    OrdinalKind
	Effort  canonical.InferenceEffort
	Changes []compat.Change
}

// ProjectOrdinalReasoning preserves enablement separately from intensity.
// Explicit effort dominates automatic or budget-derived intensity, while
// disabled compute remains a hard-off constraint rather than a low ordinal.
func ProjectOrdinalReasoning(
	reasoning canonical.ReasoningControls,
	effort canonical.Specified[canonical.InferenceEffort],
) OrdinalProjection {
	compute, computeSpecified := reasoning.ComputeField().Get()
	explicitEffort, effortSpecified := effort.Get()

	if computeSpecified && compute.Kind() == canonical.ReasoningDisabled {
		var changes []compat.Change
		if effortSpecified {
			changes = compat.AppendUnique(changes, compat.NewOmission(
				canonical.RequestControlsEffort,
				canonical.Occurrence{},
			))
		}
		return OrdinalProjection{Kind: OrdinalDisabled, Changes: changes}
	}

	if effortSpecified {
		var changes []compat.Change
		if computeSpecified && compute.Kind() == canonical.ReasoningBudget {
			changes = compat.AppendUnique(changes, reasoningApproximation())
		}
		return OrdinalProjection{Kind: OrdinalEffort, Effort: explicitEffort, Changes: changes}
	}

	if !computeSpecified {
		return OrdinalProjection{}
	}

	switch compute.Kind() {
	case canonical.ReasoningAutomatic:
		return OrdinalProjection{Kind: OrdinalAutomatic}
	case canonical.ReasoningBudget:
		tokens, _ := compute.Tokens()
		return OrdinalProjection{
			Kind:    OrdinalEffort,
			Effort:  EffortFromReferenceReasoningBudget(tokens),
			Changes: []compat.Change{reasoningApproximation()},
		}
	default:
		return OrdinalProjection{}
	}
}

func reasoningApproximation() compat.Change {
	return compat.NewApproximation(
		canonical.RequestReasoning,

		canonical.Occurrence{})

}
