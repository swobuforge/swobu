package compat

// Outcome describes how one representation seam preserved a feature.
// Exact is equivalent meaning. Approx is an explicitly approved bounded
// degradation. Drop is legal only for an independent occurrence whose erasure
// leaves an unchanged, valid residual. Reject records that the seam could not
// preserve semantics; the owning boundary separately chooses the truthful
// error and recovery owner.
type Outcome string

const (
	Exact  Outcome = "exact"
	Approx Outcome = "approx"
	Drop   Outcome = "drop"
	Reject Outcome = "reject"
)
