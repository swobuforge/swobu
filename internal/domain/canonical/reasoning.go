package canonical

// InferenceEffort is a relative preference for total model work. It neither
// enables reasoning nor implies a token allocation.
type InferenceEffort string

const (
	InferenceEffortMinimal InferenceEffort = "minimal"
	InferenceEffortLow     InferenceEffort = "low"
	InferenceEffortMedium  InferenceEffort = "medium"
	InferenceEffortHigh    InferenceEffort = "high"
	InferenceEffortXHigh   InferenceEffort = "xhigh"
	InferenceEffortMax     InferenceEffort = "max"
)

func validInferenceEffort(value InferenceEffort) bool {
	switch value {
	case InferenceEffortMinimal, InferenceEffortLow, InferenceEffortMedium,
		InferenceEffortHigh, InferenceEffortXHigh, InferenceEffortMax:
		return true
	default:
		return false
	}
}

// ReasoningComputeKind identifies one closed reasoning-compute state.
type ReasoningComputeKind string

const (
	ReasoningDisabled  ReasoningComputeKind = "disabled"
	ReasoningAutomatic ReasoningComputeKind = "automatic"
	ReasoningBudget    ReasoningComputeKind = "budget"
)

// ReasoningCompute is one disabled, automatic, or positive-budget request.
// Private fields make contradictory activation and token states impossible.
type ReasoningCompute struct {
	kind   ReasoningComputeKind
	tokens int
}

// NewDisabledReasoningCompute requires reasoning to remain disabled.
func NewDisabledReasoningCompute() ReasoningCompute {
	return ReasoningCompute{kind: ReasoningDisabled}
}

// NewAutomaticReasoningCompute enables reasoning while leaving allocation to
// the selected provider and target.
func NewAutomaticReasoningCompute() ReasoningCompute {
	return ReasoningCompute{kind: ReasoningAutomatic}
}

// NewBudgetReasoningCompute enables reasoning with a positive numeric
// allocation. The value is not universally a hard maximum.
func NewBudgetReasoningCompute(tokens int) (ReasoningCompute, error) {
	if tokens <= 0 {
		return ReasoningCompute{}, BadRequest("reasoning budget must be greater than zero")
	}
	return ReasoningCompute{kind: ReasoningBudget, tokens: tokens}, nil
}

// Kind returns the populated compute branch.
func (c ReasoningCompute) Kind() ReasoningComputeKind {
	switch {
	case c.kind == ReasoningDisabled && c.tokens == 0:
		return ReasoningDisabled
	case c.kind == ReasoningAutomatic && c.tokens == 0:
		return ReasoningAutomatic
	case c.kind == ReasoningBudget && c.tokens > 0:
		return ReasoningBudget
	default:
		return ""
	}
}

// Tokens returns the positive budget when the budget branch is populated.
func (c ReasoningCompute) Tokens() (int, bool) {
	if c.Kind() != ReasoningBudget {
		return 0, false
	}
	return c.tokens, true
}

// ReasoningDisclosure controls readable reasoning at the client edge.
type ReasoningDisclosure string

const (
	ReasoningDisclosureNone    ReasoningDisclosure = "none"
	ReasoningDisclosureSummary ReasoningDisclosure = "summary"
)

func validReasoningDisclosure(value ReasoningDisclosure) bool {
	return value == ReasoningDisclosureNone || value == ReasoningDisclosureSummary
}

// ReasoningControlsParams carries independently specified compute and
// disclosure fields.
type ReasoningControlsParams struct {
	Compute    Specified[ReasoningCompute]
	Disclosure Specified[ReasoningDisclosure]
}

// ReasoningControls is the shallow canonical reasoning request band.
type ReasoningControls struct {
	compute    Specified[ReasoningCompute]
	disclosure Specified[ReasoningDisclosure]
}

// NewReasoningControls validates one reasoning request band.
func NewReasoningControls(params ReasoningControlsParams) (ReasoningControls, error) {
	controls := ReasoningControls{
		compute:    cloneSpecified(params.Compute, func(value ReasoningCompute) ReasoningCompute { return value }),
		disclosure: cloneSpecified(params.Disclosure, func(value ReasoningDisclosure) ReasoningDisclosure { return value }),
	}
	if compute, ok := controls.compute.Get(); ok && compute.Kind() == "" {
		return ReasoningControls{}, BadRequest("reasoning compute is invalid")
	}
	if disclosure, ok := controls.disclosure.Get(); ok && !validReasoningDisclosure(disclosure) {
		return ReasoningControls{}, BadRequest("reasoning disclosure is invalid")
	}
	if compute, computeSet := controls.compute.Get(); computeSet && compute.Kind() == ReasoningDisabled {
		if disclosure, disclosureSet := controls.disclosure.Get(); disclosureSet && disclosure != ReasoningDisclosureNone {
			return ReasoningControls{}, BadRequest("disabled reasoning conflicts with summary disclosure")
		}
	}
	return controls, nil
}

// ComputeField returns compute and its independent presence.
func (c ReasoningControls) ComputeField() Specified[ReasoningCompute] {
	return cloneSpecified(c.compute, func(value ReasoningCompute) ReasoningCompute { return value })
}

// DisclosureField returns disclosure and its independent presence.
func (c ReasoningControls) DisclosureField() Specified[ReasoningDisclosure] {
	return cloneSpecified(c.disclosure, func(value ReasoningDisclosure) ReasoningDisclosure { return value })
}

// Clone returns an independent value.
func (c ReasoningControls) Clone() ReasoningControls {
	return ReasoningControls{compute: c.ComputeField(), disclosure: c.DisclosureField()}
}

// IsZero reports whether neither request field was supplied.
func (c ReasoningControls) IsZero() bool {
	return !c.compute.IsSpecified() && !c.disclosure.IsSpecified()
}
