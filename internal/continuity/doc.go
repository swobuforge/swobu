// Package continuity resolves durable conversation continuation state.
//
// Immutable checkpoints are partitioned by workspace. The memory store retains
// a bounded process-local window and structurally shares canonical history
// prefixes; expiry and restart discard it. Exact response IDs or unique History
// matches select state. Execution affinity never does.
// Transport authentication and client encoding remain outside this package.
package continuity
