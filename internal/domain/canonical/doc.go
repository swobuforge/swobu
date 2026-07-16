// Package canonical owns Swobu's semantic center for the v0 request path.
//
// It classifies client families, normalizes paths, preserves canonical
// request/output/replay semantics, and defines the Swobu-vs-backend
// error boundary. Playback-aware flows use TurnRef so semantic chain
// ownership stays separate from provider-native turn-state bytes. Canonical requests now also own the semantic tool ontology:
// function/custom/capability declarations, execution ownership, structured
// tool identity, and tool policy are represented separately so adapters can
// lower to wire formats without collapsing distinct lifecycles into one flat
// tool blob. Tool-call payloads stay raw until the wire edge can prove the
// projection is lossless. Structured tool IDs render as
// tool:v1/{origin}/{kind}/{path}; flat wire names are deterministic
// projections of those IDs, not canonical truth.
// Canonical outputs may also carry provider-neutral token usage and cache
// accounting, including reasoning-token breakdowns when the source protocol
// exposes them, so adapters can expose runtime cost facts without provider-
// dialect leakage into core nouns.
// Canonical requests also own the semantic generation-control band:
// max_output_tokens, temperature, top_p, and stop sequences are represented
// separately so adapters can lower or reject them without inventing wire-only
// request state.
// Canonical requests also own the instruction band: provider/system/developer
// guidance is represented separately from user-authored conversation items so
// coding-agent contracts are not replayed as user requests.
// Canonical requests also own the semantic tool-call batch band:
// parallel_tool_calls:false / disable_parallel_tool_use:true lowerings are
// represented explicitly so adapters can lower or reject them without inventing
// provider-only batching semantics.
// Canonical requests also own the final-answer output-format band:
// plain text remains the default, while structured JSON Schema output is
// represented explicitly so adapters can lower or reject it without prompt
// hacks or transport-only fields.
// Request-side cache controls are not active runtime truth in v0. Transport
// decoders may accept cache-shaped fields for compatibility, but canonical
// request semantics do not preserve them.
// Protocol-edge DTOs, realized wire payloads, and transport mechanics must
// stay outside this package.
package canonical
