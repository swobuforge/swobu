// Package continuity resolves durable conversation continuation state.
//
// Immutable checkpoints are partitioned by workspace. The memory store retains
// a byte-budgeted process-local window. Linear continuations structurally share
// one private history spine; branches copy once into independent chains.
// Capacity eviction and restart discard state. Exact response IDs or unique
// History matches select state. Execution affinity never does.
// Transport authentication and client encoding remain outside this package.
package continuity
