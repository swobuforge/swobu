// Package messages translates canonical semantic requests and outputs to/from
// Anthropic-style messages wire shapes.
//
// It owns only protocol-edge mapping. Flat tool declarations use deterministic
// projected names so specific tool choice can round-trip through the wire
// surface without storing projection state. Explicit structured-output
// requests fail closed because this family does not expose an exact
// structured-output field. Canonical at_most_one tool batching lowers through
// disable_parallel_tool_use when the selected target supports the exact field.
// Provider auth, base URL, and transport behavior stay in provider wiring
// packages.
package messages
