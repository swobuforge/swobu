// Package effect owns the typed side effects emitted by pure exchange and
// boundary adapters.
//
// The pure core stays deterministic; if a transform needs to cross the
// side-effect boundary, it emits an Effect and the adapter commits it at the
// edge. Timing, evidence projection, and telemetry summarization are not
// effect kinds. Compatibility decisions and turn-state operations are the only
// long-term effect kinds today.
package effect
