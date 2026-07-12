package compat

// Outcome describes how much fidelity one feature preserved during translation.
type Outcome string

const (
	Exact  Outcome = "exact"
	Approx Outcome = "approx"
	Drop   Outcome = "drop"
	Reject Outcome = "reject"
)
