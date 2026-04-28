// Package evidence stores immutable runtime traffic events and derives small
// operator-facing projections from them.
//
// It owns append/query and projection mechanics only. It must not invent new
// evidence semantics or mutate execution behavior.
package evidence
