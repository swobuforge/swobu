// Package trafficevidence stores immutable traffic events and derives small
// operator-facing projections from them.
//
// It owns append/query and projection mechanics only. It must not invent new
// traffic semantics or mutate execution behavior.
package trafficevidence
