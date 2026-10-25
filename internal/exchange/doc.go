// Package exchange orchestrates one request across configured route targets.
// Registry Models projections freeze Connection identity/revision into provider
// snapshots before adapter use. The snapshot remains immutable across credential
// rotation; target-local generation and reusable configuration are independent.
// A provider-owned refresh exchanges material only after a positive pre-execution
// authentication rejection. A separate command stages/publishes via the configured
// application callback; a new attempt resolves and captures the resulting registry
// revision. Failed invocations remain immutable. Each candidate gets at most one
// refresh retry per exchange, with independent accounting and invocation identity.
//
// It coordinates continuation, provider attempts, local tool execution, and
// response settlement. Checkpoints are immutable fork-safe response boundaries;
// execution affinity influences placement without selecting state. Recognized provider error detail remains typed through
// attempt classification and terminal settlement; opaque backend bodies do
// not enter stable logs. Provider authentication and wire encoding remain in
// adapters. Media fetching uses an exchange-scoped resolver; fetched bytes
// are execution artifacts rather than conversation checkpoints.
// Executable local-tool streaming keeps the same reducer active behind a
// cancellation-aware, backpressured canonical delivery bridge. Safe messages
// may reach the client before round closure; local calls and intermediate
// terminals stay private. Once output is exposed, failures terminate that
// response rather than falling back into a second client stream. Checkpoint
// publication uses the final prepared causal history, not the early handoff.
// Mixed local/caller rounds finish local effects before terminal caller handoff.
// Their checkpoint request retains the complete ordered provider round followed
// by local results; an empty response marks the boundary, and only the client
// projection hides local work. Resume admits results
// for unresolved effects and uses full replay rather than a stale native handle.
// Priced provider attempts require validated target-generation/selector-matched
// quotes for the actual prepared provider request. Accounting persists price
// identity and quantity coverage before transport commands; fallback attempts
// get independent quotes. Missing pricing remains unknown. Provider terminal
// usage settles supported complete dimensions against the frozen price; missing
// dimensions retain an uncertain estimate. HTTP ingress alone is not settlement.
package exchange
