// Package messages translates canonical semantic requests and outputs to/from
// Anthropic-style messages wire shapes.
//
// It owns only protocol-edge mapping. Flat tool declarations use deterministic
// projected names so specific tool choice can round-trip through the wire
// surface without storing projection state. Empty tool surfaces omit
// tool_choice because the choice is inert there and some Anthropic-compatible
// backends reject the explicit field. Explicit structured-output requests fail
// closed because this family does not expose an exact structured-output field.
// Canonical at_most_one tool batching lowers through
// disable_parallel_tool_use inside tool_choice when the selected target
// supports the exact field.
// Canonical instructions lower to and decode from the top-level system field.
// Direct Messages lowering accepts URL and inline images; exact provider
// composition may select inline-only lowering without teaching this reusable
// grammar a provider identity.
// Provider auth, base URL, and transport behavior stay in provider wiring
// packages.
// Ordered messages define the private messages history fingerprint scheme.
// Top-level system and generation fields remain on rebased requests but do not
// identify history.
package messages
