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
// The top-level system field decodes as a request-scoped directive. Lowering
// hoists only a leading directive/declaration prefix that the static Messages
// fields can represent exactly; mid-history additions are target-local
// incompatibilities.
// Direct Messages lowering accepts URL and inline images; exact provider
// composition may select inline-only lowering without teaching this reusable
// grammar a provider identity.
// Thinking controls map to canonical compute, disclosure, and effort.
// Absent display remains unspecified; summarized and omitted are explicit.
// Signed and redacted thinking blocks complete as reasoning items only after
// their signature/data is available. Each complete block is retained as typed
// Messages opaque thinking and replays unchanged through any Messages lowerer;
// no target or model allowlist is inferred.
// Omitted client projection retains an empty thinking block and exact signature,
// preserving the native protocol graph for history replay.
// Provider auth, base URL, and transport behavior stay in provider wiring
// packages.
// Direct web search lowers to the protocol-owned versioned declaration with a
// direct caller. Exact providers may replace that typed declaration only when
// their observed wire grammar differs, before this package's single
// serialization boundary. Messages history can represent only a search action
// with exactly one query. A
// completed unrepresentable call/result lifecycle is omitted atomically with
// one compatibility decision, while an unresolved call rejects so routing may
// fall back. The strict leaf call encoder never fabricates or skips an item.
// Web-search result content preserves the Messages wire union: successful
// searches use an array of result blocks, while failures use one error object.
// Citation cited_text maps to canonical citation evidence, and Messages rune
// indexes convert to canonical UTF-8 byte offsets at this boundary.
// Ordered messages define the private messages history fingerprint scheme.
// Top-level system and generation fields remain on rebased requests but do not
// identify history.
package messages
